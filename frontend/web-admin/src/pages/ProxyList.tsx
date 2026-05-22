import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Table,
  Button,
  Space,
  Tag,
  Switch,
  Popconfirm,
  message,
  Card,
  Statistic,
  Row,
  Col,
  Typography,
} from 'antd';
import { PlusOutlined, ReloadOutlined, ApiOutlined } from '@ant-design/icons';
import {
  listProxies,
  deleteProxy,
  updateProxy,
  healthCheck,
  ProxyStatus,
} from '../api/client';

const statusColor: Record<string, string> = {
  running: 'green',
  stopped: 'default',
  error: 'red',
};

export default function ProxyList() {
  const navigate = useNavigate();
  const [proxies, setProxies] = useState<ProxyStatus[]>([]);
  const [loading, setLoading] = useState(false);
  const [health, setHealth] = useState({
    status: '',
    uptime: 0,
    proxies_running: 0,
    proxies_total: 0,
  });

  const fetchProxies = useCallback(async () => {
    setLoading(true);
    try {
      const data = await listProxies();
      setProxies(data);
    } catch {
      message.error('获取代理列表失败');
    } finally {
      setLoading(false);
    }
  }, []);

  const fetchHealth = useCallback(async () => {
    try {
      const h = await healthCheck();
      setHealth(h);
    } catch {
      /* ignore */
    }
  }, []);

  useEffect(() => {
    fetchProxies();
    fetchHealth();
    const timer = window.setInterval(() => {
      fetchProxies();
      fetchHealth();
    }, 5000);
    return () => clearInterval(timer);
  }, [fetchProxies, fetchHealth]);

  const handleDelete = async (idv: string) => {
    try {
      await deleteProxy(idv);
      message.success('代理已删除');
      fetchProxies();
    } catch {
      message.error('删除失败');
    }
  };

  const handleToggle = async (record: ProxyStatus, checked: boolean) => {
    try {
      await updateProxy(record.id, {
        name: record.name,
        remote_address: record.remote_address,
        enabled: checked,
      });
      message.success(checked ? '代理已启用' : '代理已禁用');
      fetchProxies();
    } catch {
      message.error('操作失败');
    }
  };

  const columns = [
    {
      title: '名称',
      dataIndex: 'name',
      key: 'name',
      render: (text: string, r: ProxyStatus) => (
        <a onClick={() => navigate(`/proxies/${r.id}/edit`)}>{text}</a>
      ),
    },
    {
      title: 'WebSocket 路径',
      dataIndex: 'ws_path',
      key: 'ws_path',
      ellipsis: true,
      render: (p: string) => (
        <Typography.Text code copyable>
          {p}
        </Typography.Text>
      ),
    },
    { title: '目标地址', dataIndex: 'remote_address', key: 'remote' },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 80,
      render: (s: string) => <Tag color={statusColor[s]}>{s}</Tag>,
    },
    { title: '隧道连接数', dataIndex: 'connections', key: 'conn', width: 100 },
    {
      title: '启用',
      key: 'enabled',
      width: 70,
      render: (_: unknown, r: ProxyStatus) => (
        <Switch
          size="small"
          checked={r.enabled}
          onChange={(v) => handleToggle(r, v)}
        />
      ),
    },
    {
      title: '操作',
      key: 'action',
      width: 160,
      render: (_: unknown, r: ProxyStatus) => (
        <Space>
          <Button size="small" onClick={() => navigate(`/proxies/${r.id}/edit`)}>
            编辑
          </Button>
          <Popconfirm title="确定删除?" onConfirm={() => handleDelete(r.id)}>
            <Button size="small" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <>
      <Row gutter={16} style={{ marginBottom: 24 }}>
        <Col span={8}>
          <Card>
            <Statistic
              title="运行中的代理条目"
              value={health.proxies_running}
              suffix={`/ ${health.proxies_total}`}
              prefix={<ApiOutlined />}
            />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic
              title="服务端运行时长"
              value={Math.floor(health.uptime / 3600)}
              suffix="小时"
            />
          </Card>
        </Col>
      </Row>
      <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
        入口统一在服务端的 HTTP/WebSocket 端口；每条代理的路径为{' '}
        <Typography.Text code>/ws/&lt;代理ID&gt;</Typography.Text>。
      </Typography.Paragraph>
      <Space style={{ marginBottom: 16 }}>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => navigate('/proxies/new')}>
          新建代理
        </Button>
        <Button icon={<ReloadOutlined />} onClick={fetchProxies}>
          刷新
        </Button>
      </Space>
      <Table
        columns={columns}
        dataSource={proxies}
        rowKey="id"
        loading={loading}
        pagination={{ pageSize: 10 }}
      />
    </>
  );
}
