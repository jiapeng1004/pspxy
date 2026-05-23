import {useCallback, useEffect, useRef, useState} from 'react';
import {
    Typography,
    Form,
    Input,
    Button,
    Card,
    Space,
    Alert,
    Table,
    Tag,
    Modal,
    InputNumber,
    Switch,
    message,
    Popconfirm,
} from 'antd';
import type {ColumnsType} from 'antd/es/table';
import {
    CopyOutlined,
    DeleteOutlined,
    PlusOutlined,
    SafetyCertificateOutlined,
    ThunderboltOutlined,
} from '@ant-design/icons';
import type {MultiTunnelStatus, ReverseProviderPayload, ReverseProviderRow} from './api/tunnel';
import {removeReverseProvider, startReverseProvider} from './api/tunnel';
import {fetchRemoteAuthRequired, canonicalWebSocketServerUrl, resolveServerHttpOrigin} from './api/server-auth';
import {clearTunnelCreds, loadTunnelCreds, saveTunnelCreds} from './api/tunnelCredSession';

export type ReverseTunnelPanelProps = {
    status: MultiTunnelStatus | null;
    onReplaceStatus: (s: MultiTunnelStatus) => void;
};

type AuthProbe =
    | { status: 'idle' }
    | { status: 'loading'; target: string }
    | { status: 'ok'; target: string; authRequired: boolean }
    | { status: 'error'; target: string; reason: string };

function applyPersistHints(s: MultiTunnelStatus) {
    if (s.persist_warning) {
        message.warning(s.persist_warning);
    }
}

function axiosErrorMessage(e: unknown): string {
    const err = e as {
        response?: { data?: { error?: string } };
        message?: string;
    };
    return String(err.response?.data?.error || err.message || e);
}

function urlsMatchProbeTarget(formUrl: string, probeTarget: string): boolean {
    const a = formUrl.trim();
    const b = probeTarget.trim();
    if (!a || !b) return false;
    if (a === b) return true;
    return canonicalWebSocketServerUrl(a) === canonicalWebSocketServerUrl(b);
}

type ProvForm = ReverseProviderPayload & { api_key?: string };

/**
 * 出站暴露：本机已有 TCP 服务由客户端主动连 `GET /ws/rtunnel/provider` 挂到服务端；
 * 服务端下发隧道 UUID，供他人在「隧道穿透」页订阅为本地监听。
 */
export default function ReverseTunnelPanel({status, onReplaceStatus}: ReverseTunnelPanelProps) {
    const prov = status?.reverse_providers ?? [];
    const [modalOpen, setModalOpen] = useState(false);
    const [busy, setBusy] = useState(false);
    const [rowToggleBusy, setRowToggleBusy] = useState<string | null>(null);
    const [editing, setEditing] = useState<ReverseProviderRow | null>(null);
    const [form] = Form.useForm<ProvForm>();
    const [probe, setProbe] = useState<AuthProbe>({status: 'idle'});
    const probeSeq = useRef(0);

    const watchedUrl = Form.useWatch('server_url', form);

    const runProbe = useCallback(async (raw: string) => {
        const trimmed = raw.trim();
        if (!trimmed) {
            setProbe({status: 'idle'});
            return;
        }
        if (!resolveServerHttpOrigin(trimmed)) {
            setProbe({
                status: 'error',
                target: trimmed,
                reason: '无法解析为有效的 ws(s)://、http(s):// 或 主机:端口',
            });
            return;
        }
        const my = ++probeSeq.current;
        setProbe({status: 'loading', target: trimmed});
        const r = await fetchRemoteAuthRequired(trimmed);
        if (my !== probeSeq.current) return;
        if (!r.ok) setProbe({status: 'error', target: trimmed, reason: r.reason});
        else setProbe({status: 'ok', target: trimmed, authRequired: r.authRequired});
        if (!r.ok) return;
        if (!r.authRequired) clearTunnelCreds(trimmed);
    }, []);

    useEffect(() => {
        const trimmed = typeof watchedUrl === 'string' ? watchedUrl.trim() : '';
        if (!trimmed) {
            setProbe({status: 'idle'});
            return;
        }
        const t = window.setTimeout(() => void runProbe(trimmed), 400);
        return () => window.clearTimeout(t);
    }, [watchedUrl, runProbe]);

    const trimmedP = typeof watchedUrl === 'string' ? watchedUrl.trim() : '';
    const probeTargetPT =
        probe.status !== 'idle' ? String(probe.target ?? '').trim() : '';
    const probeMatches = !!trimmedP && !!probeTargetPT && urlsMatchProbeTarget(trimmedP, probeTargetPT);
    const authReady =
        probe.status !== 'loading' &&
        (!!trimmedP ? probeMatches && (probe.status === 'ok' || probe.status === 'error') : true);
    const authUnsettled = !!trimmedP && probe.status !== 'loading' && trimmedP !== '' && !authReady;
    const mandatoryAuth =
        probe.status === 'ok' &&
        probe.authRequired &&
        urlsMatchProbeTarget(trimmedP, probeTargetPT);
    const bannerUrlTrim = trimmedP || probeTargetPT;
    const showCredInputs =
        modalOpen &&
        (mandatoryAuth || (probe.status === 'error' && !!bannerUrlTrim));

    const openNew = () => {
        setEditing(null);
        form.resetFields();
        form.setFieldsValue({
            server_url: 'ws://127.0.0.1:3000',
            local_host: '127.0.0.1',
            local_port: 9090,
            api_key: '',
            enabled: true,
        });
        setModalOpen(true);
        void runProbe('ws://127.0.0.1:3000');
    };

    const openEdit = (row: ReverseProviderRow) => {
        setEditing(row);
        const rawSu = row.server_url ?? '';
        const canon = canonicalWebSocketServerUrl(rawSu);
        const displaySu = (canon || rawSu || '').trim();
        const cred = loadTunnelCreds(rawSu) ?? loadTunnelCreds(displaySu);
        form.resetFields();
        form.setFieldsValue({
            id: row.id,
            server_url: displaySu || undefined,
            local_host: row.local_host ?? '127.0.0.1',
            local_port: row.local_port ?? 1,
            api_key: cred?.api_key ?? '',
            enabled: row.enabled ?? true,
        });
        setModalOpen(true);
        setProbe({status: 'idle'});
        if (displaySu) void runProbe(displaySu);
    };

    async function finalizeApiKeyProbe(serverUrl: string, probeState: AuthProbe, apiKeyIn: string) {
        const srv = serverUrl.trim();
        let apiK = apiKeyIn.trim();
        const mandatory =
            probeState.status === 'ok' &&
            probeState.authRequired &&
            urlsMatchProbeTarget(srv, probeState.target);
        if (mandatory) {
            if (!apiK) throw new Error('该服务端已启用鉴权，请填写 api_key');
            saveTunnelCreds(srv, {api_key: apiK});
        } else if (probeState.status === 'error') {
            if (apiK) saveTunnelCreds(srv, {api_key: apiK});
            else {
                clearTunnelCreds(srv);
                apiK = '';
            }
        } else if (probeState.status === 'ok' && !probeState.authRequired) {
            clearTunnelCreds(srv);
            apiK = '';
        }
        return {apiK};
    }

    const onProvOk = async () => {
        if (authUnsettled || (probe.status === 'loading' && bannerUrlTrim)) {
            message.warning('请等待远端鉴权检测完成');
            return;
        }
        try {
            const v = await form.validateFields();
            const {apiK} = await finalizeApiKeyProbe(v.server_url, probe, String(v.api_key ?? ''));
            const payload: ReverseProviderPayload = {
                id: (v.id ?? '').trim() || undefined,
                server_url: v.server_url.trim(),
                local_host: (v.local_host ?? '').trim(),
                local_port: v.local_port,
                api_key: apiK || undefined,
                enabled: v.enabled !== false,
                channel_id: editing?.channel_id?.trim() || undefined,
            };
            setBusy(true);
            const s = await startReverseProvider(payload);
            onReplaceStatus(s);
            applyPersistHints(s);
            if (!(v.enabled !== false)) {
                message.success('已保存为禁用（未发起握手）');
            } else if (s.issued_reverse_channel) {
                try {
                    await navigator.clipboard.writeText(s.issued_reverse_channel);
                    message.success(`已暴露，隧道 ID 已复制：${s.issued_reverse_channel}`);
                } catch {
                    message.success(`隧道 ID（发给订阅方）：${s.issued_reverse_channel}`);
                }
            } else {
                message.success('已启动出站暴露');
            }
            setModalOpen(false);
            setEditing(null);
        } catch (e: unknown) {
            if ((e as { errorFields?: unknown }).errorFields) return;
            message.error(axiosErrorMessage(e));
        } finally {
            setBusy(false);
        }
    };

    const copyChan = async (uuid: string) => {
        try {
            await navigator.clipboard.writeText(uuid);
            message.success('已复制隧道 ID');
        } catch {
            message.error('复制失败');
        }
    };

    async function persistProviderEnabled(row: ReverseProviderRow, enabled: boolean): Promise<MultiTunnelStatus> {
        const rawSu = row.server_url ?? '';
        const canon = canonicalWebSocketServerUrl(rawSu);
        const srv = (canon || rawSu || '').trim();
        const cred = loadTunnelCreds(rawSu) ?? loadTunnelCreds(srv);
        if (enabled) {
            const probe = await fetchRemoteAuthRequired(srv);
            if (probe.ok && probe.authRequired && !(cred?.api_key ?? '').trim()) {
                throw new Error('服务端已启用 api_key：请点击「配置」填写密钥后再启用');
            }
        }
        const lp = row.local_port;
        if (lp == null || lp < 1 || lp > 65535) {
            throw new Error('本机端口无效，请先用「配置」修复');
        }
        const payload: ReverseProviderPayload = {
            id: row.id,
            server_url: srv,
            local_host: (row.local_host ?? '127.0.0.1').trim(),
            local_port: lp,
            api_key: cred?.api_key ?? undefined,
            enabled,
            channel_id: row.channel_id?.trim() || undefined,
        };
        return await startReverseProvider(payload);
    }

    async function toggleProviderEnabled(row: ReverseProviderRow, enabled: boolean) {
        if (rowToggleBusy) return;
        setRowToggleBusy(row.id);
        try {
            const s = await persistProviderEnabled(row, enabled);
            onReplaceStatus(s);
            applyPersistHints(s);
            message.success(enabled ? '已启用' : '已禁用');
        } catch (e: unknown) {
            message.error(axiosErrorMessage(e));
        } finally {
            setRowToggleBusy(null);
        }
    }

    async function disableAllEnabledProviders() {
        const targets = prov.filter((r) => r.enabled);
        if (targets.length === 0) {
            message.info('没有已启用的出站暴露');
            return;
        }
        setRowToggleBusy('__bulk__');
        try {
            let last: MultiTunnelStatus | undefined;
            for (const r of targets) {
                last = await persistProviderEnabled(r, false);
            }
            if (last) {
                onReplaceStatus(last);
                applyPersistHints(last);
            }
            message.success('已全部禁用出站暴露');
        } catch (e: unknown) {
            message.error(axiosErrorMessage(e));
        } finally {
            setRowToggleBusy(null);
        }
    }

    async function removeProv(id: string) {
        try {
            const s = await removeReverseProvider(id);
            onReplaceStatus(s);
            applyPersistHints(s);
            message.success('已删除该暴露条目');
        } catch (e: unknown) {
            message.error(axiosErrorMessage(e));
        }
    }

    function probeBanner() {
        switch (probe.status) {
            case 'loading':
                return <Alert type="info" message="正在检测远端鉴权…" showIcon/>;
            case 'ok':
                return probe.authRequired ? (
                    <Alert type="warning" showIcon icon={<SafetyCertificateOutlined/>} message="服务端已启用 api_key"/>
                ) : (
                    <Alert type="success" message="未强制 api_key（或服务端未配置密钥）。" showIcon/>
                );
            case 'error':
                return (
                    <Alert
                        type="warning"
                        showIcon
                        message="未能探测鉴权，若已启用请手动填写 api_key"
                        description={probe.reason}
                    />
                );
            default:
                return bannerUrlTrim ? null : null;
        }
    }

    const runningCount = prov.filter((p) => p.running).length;

    const cols: ColumnsType<ReverseProviderRow> = [
        {
            title: '隧道 ID',
            width: 200,
            ellipsis: true,
            render: (_, r) =>
                r.channel_id ? (
                    <Space>
                        <Typography.Text code copyable ellipsis style={{maxWidth: 148}}>
                            {r.channel_id}
                        </Typography.Text>
                        <Button type="link" size="small" icon={<CopyOutlined/>}
                                onClick={() => void copyChan(r.channel_id!)}/>
                    </Space>
                ) : (
                    '—'
                ),
        },
        {
            title: '本机目标',
            width: 140,
            render: (_, r) => `${r.local_host ?? ''}:${r.local_port ?? ''}`,
        },
        {title: '服务端', ellipsis: true, dataIndex: 'server_url'},
        {
            title: '启用',
            width: 100,
            render: (_, row) => (
                <Switch
                    checked={row.enabled}
                    loading={rowToggleBusy === row.id || rowToggleBusy === '__bulk__'}
                    onChange={(v) => void toggleProviderEnabled(row, v)}
                />
            ),
        },
        {
            title: '状态',
            width: 168,
            render: (_, row) =>
                row.running ? (
                    <Tag color="success">运行中</Tag>
                ) : (
                    <Space align="start" direction="vertical" size={4}>
                        <Tag>未运行</Tag>
                        {row.error ? (
                            <Typography.Text type="danger" ellipsis={{ tooltip: row.error }}>
                                {row.error}
                            </Typography.Text>
                        ) : null}
                    </Space>
                ),
        },
        {
            title: '操作',
            width: 160,
            render: (_, row) => (
                <Space size="small" wrap>
                    <Button size="small" type="link" icon={<ThunderboltOutlined/>} onClick={() => openEdit(row)}>
                        配置
                    </Button>
                    <Popconfirm title="删除该条出站暴露？" onConfirm={() => void removeProv(row.id)}>
                        <Button size="small" type="link" danger icon={<DeleteOutlined/>}>
                            删除
                        </Button>
                    </Popconfirm>
                </Space>
            ),
        },
    ];

    return (
        <>
            <Typography.Paragraph type="secondary">
                用于<strong>服务端打不到的本机端口</strong>：由本机主动向{' '}
                <Typography.Text code>/ws/rtunnel/provider</Typography.Text>{' '}
                挂载；成功后服务端下发<strong>隧道 ID</strong>（UUID）。需要访问该服务的人请到{' '}
                <strong>隧道穿透</strong>页「订阅动态隧道」填写此 ID，不在本页操作。
            </Typography.Paragraph>

            <Card
                title="出站暴露 · 条目"
                extra={
                    <Space wrap>
                        <Typography.Text type="secondary">
                            运行 {runningCount}/{prov.length}
                        </Typography.Text>
                        <Button type="primary" icon={<PlusOutlined/>} onClick={openNew}>
                            添加出站暴露
                        </Button>
                        <Button onClick={() => void disableAllEnabledProviders()}>全部禁用</Button>
                    </Space>
                }
            >
                <Table<ReverseProviderRow>
                    rowKey="id"
                    size="small"
                    pagination={false}
                    locale={{emptyText: '暂无：本页只负责「把已有本地端口交给服务端路由」'}}
                    dataSource={prov}
                    columns={cols}
                />
            </Card>

            <Modal
                title={editing ? '出站暴露 · 配置' : '出站暴露 · 新增'}
                open={modalOpen}
                confirmLoading={busy}
                onCancel={() => {
                    setModalOpen(false);
                    setEditing(null);
                }}
                onOk={() => void onProvOk()}
                okButtonProps={{
                    disabled: authUnsettled || (probe.status === 'loading' && !!bannerUrlTrim.trim()),
                }}
                width={560}
            >
                <Space direction="vertical" style={{width: '100%', marginBottom: 12}}>
                    {bannerUrlTrim ? probeBanner() : null}
                </Space>
                <Form form={form} layout="vertical">
                    <Form.Item name="id" hidden>
                        <Input/>
                    </Form.Item>
                    <Form.Item label="服务端 WebSocket 基址">
                        <Space.Compact style={{width: '100%'}}>
                            <Form.Item
                                name="server_url"
                                noStyle
                                rules={[{required: true, message: '例如 ws://host:3000'}]}
                            >
                                <Input style={{flex: 1}}/>
                            </Form.Item>
                            <Button type="default"
                                    onClick={() => void runProbe(String(form.getFieldValue('server_url')))}>
                                检测鉴权
                            </Button>
                        </Space.Compact>
                    </Form.Item>
                    <Form.Item label="要暴露的本机主机" name="local_host" initialValue="127.0.0.1">
                        <Input placeholder="127.0.0.1"/>
                    </Form.Item>
                    <Form.Item label="要暴露的本机 TCP 端口" name="local_port"
                               rules={[{required: true, type: 'number', min: 1}]}>
                        <InputNumber style={{width: '100%'}} placeholder="例如 9090（服务已监听在此端口）"/>
                    </Form.Item>
                    <Form.Item
                        label="启用"
                        name="enabled"
                        valuePropName="checked"
                        tooltip="关闭即禁用：停止连接并写入配置，客户端重启后也不会自动握手；开启则尝试连接并自动恢复。"
                    >
                        <Switch/>
                    </Form.Item>
                    {showCredInputs ? (
                        <>
                            <Typography.Text type="secondary" style={{display: 'block', marginBottom: 8}}>
                                API Key（与服务端任选密钥片段一致）
                            </Typography.Text>
                            <Form.Item
                                label="API Key"
                                name="api_key"
                                rules={mandatoryAuth ? [{required: true, message: '必填'}] : undefined}
                            >
                                <Input.Password autoComplete="off"/>
                            </Form.Item>
                        </>
                    ) : null}
                </Form>
            </Modal>
        </>
    );
}
