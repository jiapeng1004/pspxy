import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Typography,
  Form,
  Input,
  InputNumber,
  Button,
  Card,
  Space,
  Alert,
  Table,
  Tag,
  Modal,
  message,
  Popconfirm,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  PlusOutlined,
  StopOutlined,
  ThunderboltOutlined,
  DeleteOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons';
import type { TunnelConfig, ProxyRow, MultiTunnelStatus } from './api/tunnel';
import {
  fetchTunnelStatus,
  startProxy,
  stopProxy,
  removeProxy,
} from './api/tunnel';
import { fetchRemoteAuthRequired, canonicalWebSocketServerUrl, resolveServerHttpOrigin } from './api/server-auth';
import { clearTunnelCreds, loadTunnelCreds, saveTunnelCreds } from './api/tunnelCredSession';

type AuthProbe =
  | { status: 'idle' }
  | { status: 'loading'; target: string }
  | { status: 'ok'; target: string; authRequired: boolean }
  | { status: 'error'; target: string; reason: string };

type PendingModalFill =
  | { kind: 'edit'; row: ProxyRow }
  | {
      kind: 'new';
      presets: TunnelConfig & { access_key?: string; secret_key?: string };
    };

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

/** 表单里的地址是否与本次探测所用的 target 等价（兼容裸 host / 前缀差异）。 */
function urlsMatchProbeTarget(formUrl: string, probeTarget: string): boolean {
  const a = formUrl.trim();
  const b = probeTarget.trim();
  if (!a || !b) return false;
  if (a === b) return true;
  return canonicalWebSocketServerUrl(a) === canonicalWebSocketServerUrl(b);
}

export default function App() {
  const [status, setStatus] = useState<MultiTunnelStatus | null>(null);
  const [modalOpen, setModalOpen] = useState(false);
  const [pendingFillTick, setPendingFillTick] = useState(0);
  const [editingNew, setEditingNew] = useState(false);
  const [modalBusy, setModalBusy] = useState(false);
  const [form] = Form.useForm<TunnelConfig & { access_key?: string; secret_key?: string }>();
  const [probe, setProbe] = useState<AuthProbe>({ status: 'idle' });
  const probeSeq = useRef(0);
  const pendingModalRef = useRef<PendingModalFill | null>(null);
  const modalFillGen = useRef(0);

  const watchedUrl = Form.useWatch('server_url', form);

  const runProbe = useCallback(async (serverUrlRaw: string) => {
    const trimmed = serverUrlRaw.trim();
    if (!trimmed) {
      setProbe({ status: 'idle' });
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
    setProbe({ status: 'loading', target: trimmed });
    const r = await fetchRemoteAuthRequired(trimmed);
    if (my !== probeSeq.current) return;
    if (!r.ok) {
      setProbe({ status: 'error', target: trimmed, reason: r.reason });
      return;
    }
    setProbe({ status: 'ok', target: trimmed, authRequired: r.authRequired });
    if (!r.authRequired) {
      clearTunnelCreds(trimmed);
    }
  }, []);

  useEffect(() => {
    const trimmed = typeof watchedUrl === 'string' ? watchedUrl.trim() : '';
    if (!trimmed) {
      setProbe({ status: 'idle' });
      return;
    }
    const t = window.setTimeout(() => void runProbe(trimmed), 400);
    return () => window.clearTimeout(t);
  }, [watchedUrl, runProbe]);

  const poll = useCallback(async () => {
    try {
      setStatus(await fetchTunnelStatus());
    } catch {
      /* 客户端未运行时忽略 */
    }
  }, []);

  useEffect(() => {
    poll();
    const id = window.setInterval(poll, 2000);
    return () => clearInterval(id);
  }, [poll]);

  const watchedTrim = typeof watchedUrl === 'string' ? watchedUrl.trim() : '';
  const probeTargetTrim =
    probe.status !== 'idle' ? String(probe.target ?? '').trim() : '';
  const probeMatchesForm =
    !!watchedTrim && !!probeTargetTrim && urlsMatchProbeTarget(watchedTrim, probeTargetTrim);
  /** 远端已确认要 AK/SK 时一律展示凭证行，避免仅靠「表单值与 probe 字符串完全一致」才渲染。 */
  const credFieldsMandatory = probe.status === 'ok' && probe.authRequired;
  const showCredInputs =
    modalOpen &&
    (credFieldsMandatory ||
      (probe.status === 'error' && !!(watchedTrim || probeTargetTrim)));
  const bannerUrlTrim = watchedTrim || probeTargetTrim;
  const authReady =
    probeMatchesForm &&
    (probe.status === 'ok' || probe.status === 'error');
  const authUnsettled = !!watchedTrim && probe.status !== 'loading' && !authReady;
  const authChecking = probe.status === 'loading' && probeMatchesForm;

  const closeModal = () => {
    modalFillGen.current += 1;
    pendingModalRef.current = null;
    setModalOpen(false);
    setEditingNew(false);
  };

  const openAdd = () => {
    setEditingNew(true);
    pendingModalRef.current = {
      kind: 'new',
      presets: {
        id: crypto.randomUUID(),
        server_url: 'ws://127.0.0.1:3000',
        proxy_id: '',
        local_port: 10808,
        access_key: '',
        secret_key: '',
      },
    };
    setModalOpen(true);
    setPendingFillTick((x) => x + 1);
  };

  const openEdit = (row: ProxyRow) => {
    setEditingNew(false);
    pendingModalRef.current = { kind: 'edit', row };
    setModalOpen(true);
    setPendingFillTick((x) => x + 1);
  };

  /** 表单挂载需在 Modal commit 之后，用 effect + tick 回填比 afterOpenChange 更稳定。 */
  useEffect(() => {
    if (!modalOpen) return;
    const p = pendingModalRef.current;
    if (!p) return;
    const gen = modalFillGen.current;
    const t = window.setTimeout(() => {
      if (gen !== modalFillGen.current) return;
      if (p.kind === 'edit') {
        const row = p.row;
        const rawSu = row.last_config?.server_url ?? '';
        const canonSu = canonicalWebSocketServerUrl(rawSu);
        const displaySu = (canonSu || rawSu || '').trim();
        const cred = loadTunnelCreds(rawSu) ?? loadTunnelCreds(displaySu || rawSu);
        form.resetFields();
        form.setFieldsValue({
          id: row.id,
          server_url: displaySu || undefined,
          proxy_id: row.last_config.proxy_id,
          local_port: row.last_config.local_port,
          access_key: cred?.access_key ?? '',
          secret_key: cred?.secret_key ?? '',
        });
        setProbe({ status: 'idle' });
        if (displaySu) void runProbe(displaySu);
      } else {
        form.resetFields();
        form.setFieldsValue(p.presets);
        setProbe({ status: 'idle' });
        void runProbe(String(p.presets.server_url ?? '').trim());
      }
      pendingModalRef.current = null;
    }, 0);
    return () => window.clearTimeout(t);
  }, [modalOpen, pendingFillTick, form, runProbe]);

  const onModalOk = async () => {
    if (authUnsettled || authChecking) {
      message.warning('请等待远端鉴权检测完成');
      return;
    }
    try {
      const v = await form.validateFields();
      const srv = v.server_url.trim();
      let ak = (v.access_key ?? '').trim();
      let sk = (v.secret_key ?? '').trim();

      const mandatoryAuth =
        probe.status === 'ok' && probe.authRequired && urlsMatchProbeTarget(srv, probe.target);
      if (mandatoryAuth) {
        if (!ak || !sk) {
          message.error('该服务端已启用鉴权，请填写 AK/SK');
          return;
        }
        saveTunnelCreds(srv, ak, sk);
      } else if (probe.status === 'error') {
        if (ak && sk) saveTunnelCreds(srv, ak, sk);
        else {
          clearTunnelCreds(srv);
          ak = '';
          sk = '';
        }
      } else if (probe.status === 'ok' && !probe.authRequired) {
        clearTunnelCreds(srv);
        ak = '';
        sk = '';
      }

      const payload: TunnelConfig = {
        id: (v.id ?? '').trim() || undefined,
        server_url: srv,
        proxy_id: (v.proxy_id ?? '').trim(),
        local_port: v.local_port,
        access_key: ak || undefined,
        secret_key: sk || undefined,
      };

      setModalBusy(true);
      const s = await startProxy(payload);
      setStatus(s);
      applyPersistHints(s);
      message.success(editingNew ? '已添加并尝试启动' : '已更新并尝试启动');
      closeModal();
    } catch (e: unknown) {
      if ((e as { errorFields?: unknown }).errorFields) return;
      message.error(axiosErrorMessage(e));
    } finally {
      setModalBusy(false);
    }
  };

  const onStopOne = async (id: string) => {
    try {
      const s = await stopProxy({ id });
      setStatus(s);
      applyPersistHints(s);
      message.success('已停止监听');
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
    }
  };

  const onStopAll = async () => {
    try {
      const s = await stopProxy({ stop_all: true });
      setStatus(s);
      applyPersistHints(s);
      message.success('已全部停止监听');
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
    }
  };

  const onRemove = async (id: string) => {
    try {
      const s = await removeProxy(id);
      setStatus(s);
      applyPersistHints(s);
      message.success('已删除该代理配置');
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
    }
  };

  const runningCount = useMemo(
    () => status?.proxies.filter((p) => p.running).length ?? 0,
    [status]
  );

  const columns: ColumnsType<ProxyRow> = [
    {
      title: '本地监听',
      width: 160,
      render: (_, r) => (r.listen ? r.listen : `127.0.0.1:${r.last_config.local_port}`),
    },
    {
      title: '服务端',
      ellipsis: true,
      render: (_, r) => r.last_config.server_url,
    },
    {
      title: '远端代理 ID',
      ellipsis: true,
      render: (_, r) => r.last_config.proxy_id,
    },
    {
      title: '状态',
      width: 96,
      render: (_, r) =>
        r.running ? <Tag color="success">运行中</Tag> : <Tag>已停止</Tag>,
    },
    {
      title: '连接数',
      width: 72,
      dataIndex: 'active_connections',
    },
    {
      title: '操作',
      width: 268,
      render: (_, r) => (
        <Space size="small" wrap>
          <Button
            size="small"
            type="link"
            icon={<ThunderboltOutlined />}
            onClick={() => openEdit(r)}
          >
            配置/启动
          </Button>
          <Button
            size="small"
            type="link"
            danger
            icon={<StopOutlined />}
            disabled={!r.running}
            onClick={() => void onStopOne(r.id)}
          >
            停止
          </Button>
          <Popconfirm title="确定删除该条配置？" onConfirm={() => void onRemove(r.id)}>
            <Button size="small" type="link" danger icon={<DeleteOutlined />}>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  const probeAlert = useMemo(() => {
    if (!modalOpen || !bannerUrlTrim) return null;
    switch (probe.status) {
      case 'loading':
        return <Alert type="info" message="正在检测远端鉴权…" showIcon />;
      case 'ok':
        return probe.authRequired ? (
          <Alert
            type="warning"
            showIcon
            icon={<SafetyCertificateOutlined />}
            message="该服务端已开启 AK/SK，请在下方填写；握手时将附带签名。"
          />
        ) : (
          <Alert type="success" message="探测：当前未强制 AK/SK（或未配置密钥）。" showIcon />
        );
      case 'error':
        return (
          <Alert
            type="warning"
            message="未能自动探测鉴权，若已开启请填写 AK/SK"
            description={probe.reason}
            showIcon
          />
        );
      default:
        return null;
    }
  }, [modalOpen, bannerUrlTrim, probe]);

  return (
    <div style={{ maxWidth: 1100, margin: '24px auto', padding: '0 16px' }}>
      <Typography.Title level={3}>TCP Proxy 客户端</Typography.Title>
      <Typography.Paragraph type="secondary">
        可同时管理<strong>多条</strong>转发：每条占用一个本地端口，对应远端 <Typography.Text code>/ws/&lt;代理ID&gt;</Typography.Text>。
        列表与本机 <Typography.Text code>config-client.yaml</Typography.Text> 中的{' '}
        <Typography.Text code>proxies</Typography.Text> 数组同步持久化。
      </Typography.Paragraph>

      <Card
        title="代理列表"
        extra={
          <Space wrap>
            <Typography.Text type="secondary">
              运行中 {runningCount} / {status?.proxies.length ?? 0}
            </Typography.Text>
            <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>
              添加代理
            </Button>
            <Button onClick={() => void onStopAll()}>全部停止</Button>
          </Space>
        }
      >
        <Table<ProxyRow>
          rowKey="id"
          size="small"
          pagination={false}
          locale={{ emptyText: '暂无代理，点击「添加代理」' }}
          dataSource={status?.proxies ?? []}
          columns={columns}
        />
      </Card>

      <Modal
        title={editingNew ? '添加代理' : '配置代理'}
        open={modalOpen}
        forceRender
        onCancel={closeModal}
        onOk={() => void onModalOk()}
        confirmLoading={modalBusy}
        okButtonProps={{ disabled: authUnsettled || authChecking }}
        width={560}
      >
        <Space direction="vertical" style={{ width: '100%', marginBottom: 12 }}>
          {probeAlert}
        </Space>
        <Form form={form} layout="vertical">
          <Form.Item name="id" hidden>
            <Input />
          </Form.Item>
          {/* Space.Compact 不能作为绑定字段的直接子结点，否则会收走 value/onChange 却不下传到 Input */}
          <Form.Item label="服务端 WebSocket 基址">
            <Space.Compact style={{ width: '100%' }}>
              <Form.Item
                name="server_url"
                noStyle
                rules={[{ required: true, message: '例如 ws://host:3000' }]}
              >
                <Input style={{ flex: 1 }} placeholder="ws://192.168.1.10:3000" />
              </Form.Item>
              <Button
                type="default"
                onClick={() => void runProbe(String(form.getFieldValue('server_url') ?? '').trim())}
              >
                检测鉴权
              </Button>
            </Space.Compact>
          </Form.Item>
          <Form.Item
            label="远端代理 ID"
            name="proxy_id"
            rules={[{ required: true, message: '管理端创建的 UUID' }]}
          >
            <Input placeholder="uuid" />
          </Form.Item>
          <Form.Item
            label="本地监听端口"
            name="local_port"
            rules={[{ required: true, type: 'number', min: 1, max: 65535 }]}
          >
            <InputNumber style={{ width: '100%' }} />
          </Form.Item>
          {showCredInputs ? (
            <>
              <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
                {credFieldsMandatory ? '访问凭证（仅存 sessionStorage）' : '可选：远端若开鉴权则填写'}
              </Typography.Text>
              <Form.Item
                label="Access Key"
                name="access_key"
                rules={credFieldsMandatory ? [{ required: true, message: '必填' }] : undefined}
              >
                <Input autoComplete="off" />
              </Form.Item>
              <Form.Item
                label="Secret Key"
                name="secret_key"
                rules={credFieldsMandatory ? [{ required: true, message: '必填' }] : undefined}
              >
                <Input.Password autoComplete="new-password" />
              </Form.Item>
            </>
          ) : null}
        </Form>
      </Modal>
    </div>
  );
}
