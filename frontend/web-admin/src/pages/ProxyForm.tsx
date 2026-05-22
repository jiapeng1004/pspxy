import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import {
  Form,
  Input,
  Switch,
  Button,
  message,
  Card,
  Spin,
  Alert,
  Typography,
} from 'antd';
import { createProxy, updateProxy, getProxy, ProxyStatus } from '../api/client';

export default function ProxyForm() {
  const navigate = useNavigate();
  const { id } = useParams<{ id: string }>();
  const [form] = Form.useForm();
  const [loading, setLoading] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const isEdit = Boolean(id);

  useEffect(() => {
    if (id) {
      setLoading(true);
      getProxy(id)
        .then((p: ProxyStatus) => form.setFieldsValue({
          name: p.name,
          remote_address: p.remote_address,
          enabled: p.enabled,
        }))
        .catch(() => message.error('加载代理信息失败'))
        .finally(() => setLoading(false));
    }
  }, [id, form]);

  const handleSubmit = async (values: Record<string, unknown>) => {
    setSubmitting(true);
    try {
      const req = {
        name: values.name as string,
        remote_address: values.remote_address as string,
        enabled: values.enabled as boolean,
      };

      if (isEdit && id) {
        await updateProxy(id, req);
        message.success('代理已更新');
      } else {
        await createProxy(req);
        message.success('代理已创建');
      }
      navigate('/proxies');
    } catch {
      message.error('操作失败');
    } finally {
      setSubmitting(false);
    }
  };

  if (loading) {
    return <Spin size="large" style={{ display: 'block', margin: '100px auto' }} />;
  }

  return (
    <Card title={isEdit ? '编辑代理' : '新建代理'}>
      <Alert
        type="info"
        showIcon
        style={{ marginBottom: 24 }}
        message="服务端监听端口为全局唯一（见 config.yaml 的 server.port）"
        description="每条代理通过在「同一端口」上的 WebSocket 路径区分：`/ws/<代理 ID>` → 转发到下方的目标 TCP。客户端请将隧道连到正确路径。"
      />
      {isEdit && id ? (
        <Typography.Paragraph style={{ marginBottom: 16 }}>
          <Typography.Text strong>接入路径：</Typography.Text>{' '}
          <Typography.Text code copyable>
            /ws/{id}
          </Typography.Text>
        </Typography.Paragraph>
      ) : null}
      <Form
        form={form}
        layout="vertical"
        onFinish={handleSubmit}
        initialValues={{ enabled: true }}
      >
        <Form.Item
          label="名称"
          name="name"
          rules={[{ required: true, message: '请输入代理名称' }]}
        >
          <Input placeholder="例如：数据库代理" />
        </Form.Item>
        <Form.Item
          label="目标 TCP 地址"
          name="remote_address"
          rules={[{ required: true, message: '请输入 host:port' }]}
        >
          <Input placeholder="例如：127.0.0.1:3306 或 db.internal:3306" />
        </Form.Item>
        <Form.Item label="启用" name="enabled" valuePropName="checked">
          <Switch />
        </Form.Item>
        <Form.Item>
          <Button type="primary" htmlType="submit" loading={submitting}>
            提交
          </Button>
          <Button style={{ marginLeft: 12 }} onClick={() => navigate('/proxies')}>
            取消
          </Button>
        </Form.Item>
      </Form>
    </Card>
  );
}
