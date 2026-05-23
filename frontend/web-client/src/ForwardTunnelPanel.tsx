import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
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
  message,
  Popconfirm,
  Switch,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import {
  PlusOutlined,
  StopOutlined,
  ThunderboltOutlined,
  DeleteOutlined,
  SafetyCertificateOutlined,
  CopyOutlined,
} from '@ant-design/icons';
import type {
  TunnelConfig,
  ProxyRow,
  MultiTunnelStatus,
  ReverseConsumerPayload,
  ReverseConsumerRow,
} from './api/tunnel';
import {
  startProxy,
  stopProxy,
  removeProxy,
  startReverseConsumer,
  removeReverseConsumer,
} from './api/tunnel';
import { fetchRemoteAuthRequired, canonicalWebSocketServerUrl, resolveServerHttpOrigin } from './api/server-auth';
import { clearTunnelCreds, loadTunnelCreds, saveTunnelCreds } from './api/tunnelCredSession';

export type ForwardTunnelPanelProps = {
  status: MultiTunnelStatus | null;
  onReplaceStatus: (s: MultiTunnelStatus) => void;
};

type AuthProbe =
  | { status: 'idle' }
  | { status: 'loading'; target: string }
  | { status: 'ok'; target: string; authRequired: boolean }
  | { status: 'error'; target: string; reason: string };

/** 单行展示：YAML 底层仍可能在 proxies / reverse_consumers 两边，语义一致 */
type TunnelSubscribeRow =
  | { key: string; backend: 'proxy'; row: ProxyRow }
  | { key: string; backend: 'consumer'; row: ReverseConsumerRow };

type SubModalValues = {
  id?: string;
  backend?: 'proxy' | 'consumer' | '';
  server_url: string;
  tunnel_id: string;
  listen: string;
  api_key?: string;
  /** 仅 reverse_consumers 订阅条目；历史 proxy 行不使用 */
  enabled?: boolean;
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

function urlsMatchProbeTarget(formUrl: string, probeTarget: string): boolean {
  const a = formUrl.trim();
  const b = probeTarget.trim();
  if (!a || !b) return false;
  if (a === b) return true;
  return canonicalWebSocketServerUrl(a) === canonicalWebSocketServerUrl(b);
}

/** 端口数字或 host:port；纯数字等价于 127.0.0.1:端口 */
function normalizeListenAddr(raw: string): string {
  const t = raw.trim();
  if (!t) throw new Error('本地监听不能为空');
  if (/^\d+$/.test(t)) {
    const p = parseInt(t, 10);
    if (p < 1 || p > 65535) throw new Error('端口必须在 1-65535');
    return `127.0.0.1:${p}`;
  }
  if (!/^[^:]+:\d+$/.test(t)) {
    throw new Error('请填写端口数字，或 host:端口');
  }
  return t;
}

function parseLocalPortForLegacyProxy(normalizedListen: string): number {
  const m = /^127\.0\.0\.1:(\d+)$/i.exec(normalizedListen.trim());
  if (!m) {
    throw new Error(
      '本条为历史「代理」格式，只能通过 127.0.0.1:端口（或单独填端口数字）监听；若要绑定其它网卡请先删除本条，再新建一条订阅。'
    );
  }
  const p = parseInt(m[1], 10);
  if (p < 1 || p > 65535) throw new Error('端口无效');
  return p;
}

function buildSubscribeRows(status: MultiTunnelStatus | null): TunnelSubscribeRow[] {
  const out: TunnelSubscribeRow[] = [];
  for (const p of status?.proxies ?? []) {
    out.push({ key: `proxy:${p.id}`, backend: 'proxy', row: p });
  }
  for (const c of status?.reverse_consumers ?? []) {
    out.push({ key: `consumer:${c.id}`, backend: 'consumer', row: c });
  }
  out.sort((a, b) => {
    const la = listenLabel(a).toLowerCase();
    const lb = listenLabel(b).toLowerCase();
    return la.localeCompare(lb);
  });
  return out;
}

function listenLabel(r: TunnelSubscribeRow): string {
  if (r.backend === 'proxy') {
    const p = r.row;
    return p.listen || `127.0.0.1:${p.last_config.local_port}`;
  }
  return r.row.listen ?? '';
}

function tunnelIdLabel(r: TunnelSubscribeRow): string {
  return r.backend === 'proxy' ? r.row.last_config.proxy_id : r.row.id;
}

async function copyTunnelId(uuid: string) {
  const t = String(uuid ?? '').trim();
  if (!t) return;
  try {
    await navigator.clipboard.writeText(t);
    message.success('已复制隧道 ID');
  } catch {
    message.error('复制失败');
  }
}

/** 隧道穿透页：只对「订阅方」暴露统一语义 */
export default function ForwardTunnelPanel({ status, onReplaceStatus }: ForwardTunnelPanelProps) {
  const rows = useMemo(() => buildSubscribeRows(status), [status]);

  const [modalOpen, setModalOpen] = useState(false);
  const [pendingFillTick, setPendingFillTick] = useState(0);
  const [modalMode, setModalMode] = useState<'new' | 'edit'>('new');
  const [modalBusy, setModalBusy] = useState(false);
  const [form] = Form.useForm<SubModalValues>();
  const [probe, setProbe] = useState<AuthProbe>({ status: 'idle' });
  const probeSeq = useRef(0);
  const [rowToggleBusy, setRowToggleBusy] = useState<string | null>(null);
  const pendingModalRef = useRef<
    | { kind: 'new' }
    | { kind: 'edit'; entry: TunnelSubscribeRow }
    | null
  >(null);
  const modalFillGen = useRef(0);

  const watchedUrl = Form.useWatch('server_url', form);
  const watchedBackend = Form.useWatch('backend', form);
  const subFormIsConsumer = modalMode === 'new' || watchedBackend === 'consumer';
  const consumerTunnelIdFrozen = modalMode === 'edit' && watchedBackend === 'consumer';

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

  const watchedTrim = typeof watchedUrl === 'string' ? watchedUrl.trim() : '';
  const probeTargetTrim =
    probe.status !== 'idle' ? String(probe.target ?? '').trim() : '';
  const probeMatchesForm =
    !!watchedTrim && !!probeTargetTrim && urlsMatchProbeTarget(watchedTrim, probeTargetTrim);
  const credFieldsMandatory = probe.status === 'ok' && probe.authRequired;
  const showCredInputs =
    modalOpen &&
    (credFieldsMandatory ||
      (probe.status === 'error' && !!(watchedTrim || probeTargetTrim)));
  const bannerUrlTrim = watchedTrim || probeTargetTrim;
  const authReady =
    probeMatchesForm && (probe.status === 'ok' || probe.status === 'error');
  const authUnsettled = !!watchedTrim && probe.status !== 'loading' && !authReady;
  const authChecking = probe.status === 'loading' && probeMatchesForm;

  const closeModal = () => {
    modalFillGen.current += 1;
    pendingModalRef.current = null;
    setModalOpen(false);
  };

  const openAdd = () => {
    pendingModalRef.current = { kind: 'new' };
    setModalMode('new');
    setModalOpen(true);
    setPendingFillTick((x) => x + 1);
  };

  const openEdit = (entry: TunnelSubscribeRow) => {
    pendingModalRef.current = { kind: 'edit', entry };
    setModalMode('edit');
    setModalOpen(true);
    setPendingFillTick((x) => x + 1);
  };

  useEffect(() => {
    if (!modalOpen) return;
    const p = pendingModalRef.current;
    if (!p) return;
    const gen = modalFillGen.current;
    const t = window.setTimeout(() => {
      if (gen !== modalFillGen.current) return;
      if (p.kind === 'new') {
        form.resetFields();
        form.setFieldsValue({
          backend: '',
          server_url: 'ws://127.0.0.1:3000',
          tunnel_id: '',
          listen: '10808',
          api_key: '',
          enabled: true,
        });
        setProbe({ status: 'idle' });
        void runProbe('ws://127.0.0.1:3000');
      } else {
        const e = p.entry;
        const rawSu = e.backend === 'proxy' ? e.row.last_config?.server_url ?? '' : e.row.server_url ?? '';
        const canonSu = canonicalWebSocketServerUrl(rawSu);
        const displaySu = (canonSu || rawSu || '').trim();
        const cred = loadTunnelCreds(rawSu) ?? loadTunnelCreds(displaySu || rawSu);
        const listenDisp =
          e.backend === 'proxy'
            ? String(e.row.last_config.local_port ?? '')
            : String(e.row.listen ?? '');
        const tid = tunnelIdLabel(e);
        form.resetFields();
        form.setFieldsValue({
          id: e.row.id,
          backend: e.backend,
          server_url: displaySu || undefined,
          tunnel_id: tid,
          listen: listenDisp,
          api_key: cred?.api_key ?? '',
          enabled:
            e.backend === 'consumer' ? (e.row.enabled ?? true) : undefined,
        });
        setProbe({ status: 'idle' });
        if (displaySu) void runProbe(displaySu);
      }
      pendingModalRef.current = null;
    }, 0);
    return () => window.clearTimeout(t);
  }, [modalOpen, pendingFillTick, form, runProbe]);

  async function finalizeApiKeyProbe(serverUrl: string, probeState: AuthProbe, apiKeyIn: string) {
    const srv = serverUrl.trim();
    let apiK = apiKeyIn.trim();
    const mandatory =
      probeState.status === 'ok' &&
      probeState.authRequired &&
      urlsMatchProbeTarget(srv, probeState.target);
    if (mandatory) {
      if (!apiK) throw new Error('该服务端已启用鉴权，请填写 api_key');
      saveTunnelCreds(srv, { api_key: apiK });
    } else if (probeState.status === 'error') {
      if (apiK) saveTunnelCreds(srv, { api_key: apiK });
      else {
        clearTunnelCreds(srv);
        apiK = '';
      }
    } else if (probeState.status === 'ok' && !probeState.authRequired) {
      clearTunnelCreds(srv);
      apiK = '';
    }
    return { apiK };
  }

  async function persistConsumerEnabled(row: ReverseConsumerRow, enabled: boolean): Promise<MultiTunnelStatus> {
    const rawSu = row.server_url ?? '';
    const canon = canonicalWebSocketServerUrl(rawSu);
    const srv = (canon || rawSu || '').trim();
    const cred = loadTunnelCreds(rawSu) ?? loadTunnelCreds(srv);
    const listen = String(row.listen ?? '').trim();
    if (!listen) {
      throw new Error('监听地址无效，请先用「配置」修复');
    }
    const payload: ReverseConsumerPayload = {
      id: row.id,
      server_url: srv,
      listen,
      api_key: cred?.api_key ?? undefined,
      enabled,
    };
    return await startReverseConsumer(payload);
  }

  async function toggleConsumerEnabled(row: ReverseConsumerRow, enabled: boolean) {
    if (rowToggleBusy) return;
    setRowToggleBusy(row.id);
    try {
      const s = await persistConsumerEnabled(row, enabled);
      onReplaceStatus(s);
      applyPersistHints(s);
      message.success(enabled ? '已启用' : '已禁用');
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
    } finally {
      setRowToggleBusy(null);
    }
  }

  const onModalOk = async () => {
    if (authUnsettled || authChecking) {
      message.warning('请等待远端鉴权检测完成');
      return;
    }
    try {
      const v = await form.validateFields();
      const srv = String(v.server_url ?? '').trim();
      const tunnelId = String(v.tunnel_id ?? '').trim();
      if (!tunnelId) {
        message.error('请填写隧道 UUID');
        return;
      }

      let listenNorm: string;
      try {
        listenNorm = normalizeListenAddr(String(v.listen ?? ''));
      } catch (err) {
        message.error(err instanceof Error ? err.message : String(err));
        return;
      }

      let apiK = String(v.api_key ?? '').trim();
      const mandatoryAuth =
        probe.status === 'ok' && probe.authRequired && urlsMatchProbeTarget(srv, probe.target);
      if (mandatoryAuth) {
        if (!apiK) {
          message.error('该服务端已启用鉴权，请填写 api_key（与服务端任选一段完全一致）');
          return;
        }
        saveTunnelCreds(srv, { api_key: apiK });
      } else if (probe.status === 'error') {
        if (apiK) saveTunnelCreds(srv, { api_key: apiK });
        else {
          clearTunnelCreds(srv);
          apiK = '';
        }
      } else if (probe.status === 'ok' && !probe.authRequired) {
        clearTunnelCreds(srv);
        apiK = '';
      }

      setModalBusy(true);

      if (modalMode === 'new') {
        const en = v.enabled !== false;
        const payload: ReverseConsumerPayload = {
          id: tunnelId,
          server_url: srv,
          listen: listenNorm,
          api_key: apiK || undefined,
          enabled: en,
        };
        const s = await startReverseConsumer(payload);
        onReplaceStatus(s);
        applyPersistHints(s);
        message.success(
          en ? '已添加' : '已添加并保存为禁用（未监听端口）',
        );
      } else {
        const bk = String(v.backend ?? '') as TunnelSubscribeRow['backend'];
        if (bk !== 'proxy' && bk !== 'consumer') {
          message.error('内部状态异常');
          return;
        }

        const idRequired = String(v.id ?? '').trim();
        if (!idRequired) {
          message.error('缺少条目 id');
          return;
        }

        if (bk === 'consumer') {
          const en = v.enabled !== false;
          const payload: ReverseConsumerPayload = {
            id: tunnelId,
            server_url: srv,
            listen: listenNorm,
            api_key: apiK || undefined,
            enabled: en,
          };
          const s = await startReverseConsumer(payload);
          onReplaceStatus(s);
          applyPersistHints(s);
          message.success(en ? '已保存' : '已保存为禁用（未监听端口）');
        } else {
          let lp: number;
          try {
            lp = parseLocalPortForLegacyProxy(listenNorm);
          } catch (err) {
            message.error(err instanceof Error ? err.message : String(err));
            return;
          }
          const tc: TunnelConfig = {
            id: idRequired,
            server_url: srv,
            proxy_id: tunnelId,
            local_port: lp,
            api_key: apiK || undefined,
          };
          const s = await startProxy(tc);
          onReplaceStatus(s);
          applyPersistHints(s);
          message.success('已更新并重启监听');
        }
      }

      closeModal();
    } catch (e: unknown) {
      if ((e as { errorFields?: unknown }).errorFields) return;
      message.error(axiosErrorMessage(e));
    } finally {
      setModalBusy(false);
    }
  };

  const onStopRow = async (entry: TunnelSubscribeRow) => {
    if (entry.backend !== 'proxy') return;
    try {
      const s = await stopProxy({ id: entry.row.id });
      onReplaceStatus(s);
      applyPersistHints(s);
      message.success('已停止');
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
    }
  };

  const onRemoveRow = async (entry: TunnelSubscribeRow) => {
    try {
      const s =
        entry.backend === 'proxy'
          ? await removeProxy(entry.row.id)
          : await removeReverseConsumer(entry.row.id);
      onReplaceStatus(s);
      applyPersistHints(s);
      message.success('已删除');
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
    }
  };

  const onDisableAllDynamicConsumers = async () => {
    const list = [...(status?.reverse_consumers ?? [])];
    const targets = list.filter((c) => c.enabled);
    try {
      const sProx = await stopProxy({ stop_all: true });
      let cur: MultiTunnelStatus = sProx;
      if (targets.length > 0) {
        setRowToggleBusy('__bulk__');
        try {
          for (const c of targets) {
            cur = await persistConsumerEnabled(c, false);
          }
        } finally {
          setRowToggleBusy(null);
        }
      }
      onReplaceStatus(cur);
      applyPersistHints(cur);
      message.success(
        targets.length > 0
          ? '已停止全部代理监听，并已禁用全部动态订阅'
          : '已停止全部代理监听',
      );
    } catch (e: unknown) {
      message.error(axiosErrorMessage(e));
      setRowToggleBusy(null);
    }
  };

  const runningCount = rows.filter((r) => r.row.running).length;

  const cols: ColumnsType<TunnelSubscribeRow> = [
    {
      title: '本地监听',
      width: 168,
      render: (_, entry) => listenLabel(entry),
    },
    {
      title: '服务端',
      ellipsis: true,
      render: (_, entry) =>
        entry.backend === 'proxy' ? entry.row.last_config.server_url : entry.row.server_url,
    },
    {
      title: '隧道 ID',
      minWidth: 300,
      render: (_, entry) => {
        const tid = tunnelIdLabel(entry);
        return (
          <Space wrap={false}>
            <Typography.Text
              code
              ellipsis={{ tooltip: tid }}
              style={{ maxWidth: 'min(720px, 58vw)' }}
            >
              {tid}
            </Typography.Text>
            <Button type="link" size="small" icon={<CopyOutlined />} onClick={() => void copyTunnelId(tid)} />
          </Space>
        );
      },
    },
    {
      title: '启用',
      width: 88,
      render: (_, entry) =>
        entry.backend === 'consumer' ? (
          <Switch
            checked={entry.row.enabled}
            loading={rowToggleBusy === entry.row.id || rowToggleBusy === '__bulk__'}
            onChange={(v) => void toggleConsumerEnabled(entry.row, v)}
          />
        ) : (
          '—'
        ),
    },
    {
      title: '状态',
      width: 88,
      render: (_, entry) =>
        entry.row.running ? <Tag color="success">运行中</Tag> : <Tag>未运行</Tag>,
    },
    {
      title: '连接数',
      width: 80,
      render: (_, entry) => entry.row.active_connections ?? 0,
    },
    {
      title: '操作',
      width: 220,
      render: (_, entry) => (
        <Space size="small" wrap>
          <Button size="small" type="link" icon={<ThunderboltOutlined />} onClick={() => openEdit(entry)}>
            配置
          </Button>
          {entry.backend === 'proxy' ? (
            <Button
              size="small"
              type="link"
              danger
              icon={<StopOutlined />}
              disabled={!entry.row.running}
              onClick={() => void onStopRow(entry)}
            >
              停止
            </Button>
          ) : null}
          <Popconfirm title="确定删除这条订阅？" onConfirm={() => void onRemoveRow(entry)}>
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
            message="服务端已启用 api_key：请填写任选一段完全一致"
          />
        ) : (
          <Alert type="success" message="探测：未强制鉴权。" showIcon />
        );
      case 'error':
        return (
          <Alert
            type="warning"
            message="未能自动探测鉴权，若已开启请填写 api_key"
            description={probe.reason}
            showIcon
          />
        );
      default:
        return null;
    }
  }, [modalOpen, bannerUrlTrim, probe]);

  return (
    <>
      <Typography.Paragraph type="secondary">
        在本页<strong>只看到「订阅隧道」这一种事</strong>：本机起一个监听地址；每个入站 TCP 会向服务端拉起{' '}
        <Typography.Text code>GET /ws/&lt;tunnel_id&gt;</Typography.Text>
        ，由服务端决定去连管理端登记的远端 TCP、还是接住别人「出站暴露」挂上的通道——
        <strong>你只管填拿到的隧道 ID（UUID）</strong>。配置文件里条目仍可能分布在{' '}
        <Typography.Text code>proxies</Typography.Text> 与 <Typography.Text code>reverse_consumers</Typography.Text>{' '}
        两节（历史兼容），表中已合并浏览。
      </Typography.Paragraph>

      <Card
        bodyStyle={{ overflowX: 'auto' }}
        style={{ width: '100%', maxWidth: '100%' }}
        title="隧道订阅"
        extra={
          <Space wrap>
            <Typography.Text type="secondary">
              运行中 {runningCount} / {rows.length}
            </Typography.Text>
            <Button type="primary" icon={<PlusOutlined />} onClick={openAdd}>
              添加订阅
            </Button>
            <Button onClick={() => void onDisableAllDynamicConsumers()}>全部停用</Button>
          </Space>
        }
      >
        <Table<TunnelSubscribeRow>
          rowKey={(r) => r.key}
          size="small"
          pagination={false}
          locale={{ emptyText: '暂无：填服务端地址 + 隧道 UUID + 本机监听即可' }}
          dataSource={rows}
          columns={cols}
        />
      </Card>

      <Modal
        title={modalMode === 'new' ? '添加隧道订阅' : '配置隧道订阅'}
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
          <Form.Item name="backend" hidden>
            <Input />
          </Form.Item>
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
            label="隧道 UUID"
            name="tunnel_id"
            tooltip={
              modalMode === 'edit' && watchedBackend === 'consumer'
                ? '与配置文件中 id、WebSocket GET /ws/{id} 为同一 UUID；要改变隧道请先删除本条再新建。'
                : '与管理端或服务端下发的隧道 UUID 一致，并作为本条目的 id 持久化。'
            }
            rules={[{ required: true, message: '填入隧道 UUID' }]}
          >
            <Input placeholder="例如 550e8400-e29b-41d4-a716-446655440000" disabled={consumerTunnelIdFrozen} />
          </Form.Item>
          <Form.Item
            label="本机监听"
            name="listen"
            tooltip="填端口数字（默认等价 127.0.0.1:端口），或任意 host:port"
            rules={[{ required: true, message: '必填' }]}
          >
            <Input placeholder="如 10808 或 127.0.0.1:19090 或 0.0.0.0:8080" />
          </Form.Item>
          {subFormIsConsumer ? (
            <Form.Item
              label="启用"
              name="enabled"
              valuePropName="checked"
              tooltip="关闭即禁用：停止本地监听并写入配置，重启客户端后也不会自动拉起；开启则监听并会自动恢复。"
            >
              <Switch />
            </Form.Item>
          ) : null}
          {showCredInputs ? (
            <>
              <Typography.Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
                {credFieldsMandatory ? '密钥（仅存 sessionStorage）' : '可选'}
              </Typography.Text>
              <Form.Item
                label="API Key"
                name="api_key"
                rules={credFieldsMandatory ? [{ required: true, message: '必填' }] : undefined}
              >
                <Input.Password placeholder="与服务端任选一段一致" autoComplete="off" />
              </Form.Item>
            </>
          ) : null}
        </Form>
      </Modal>
    </>
  );
}
