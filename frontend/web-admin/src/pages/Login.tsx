import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom';
import { Button, Card, Form, Input, Typography, message, Spin } from 'antd';
import { UserOutlined } from '@ant-design/icons';
import { fetchAuthRequired, verifyAccessLogin } from '../api/client';
import {
  saveUnifiedApiKey,
  loadAccessCredentials,
  clearAccessCredentials,
} from '../auth/session';

/** 只允许站内相对路径，防止开放重定向 */
function safeInternalPath(raw: string | null | undefined, fallback = '/proxies'): string {
  const s = typeof raw === 'string' ? raw.trim() : '';
  if (!s.startsWith('/') || s.startsWith('//') || s.includes('://')) return fallback;
  if (s.startsWith('/login')) return fallback;
  return s || fallback;
}

export default function LoginPage() {
  const navigate = useNavigate();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const state = location.state as { from?: string } | null | undefined;

  const redirectTo = useMemo(() => {
    const q = searchParams.get('next');
    const s = state?.from;
    return safeInternalPath(q ?? s ?? undefined);
  }, [searchParams, state?.from]);

  const [booting, setBooting] = useState(true);
  const [authRequired, setAuthRequired] = useState(true);

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const required = await fetchAuthRequired();
        if (!alive) return;
        setAuthRequired(required);
        if (!required) {
          clearAccessCredentials();
          message.info('服务端未启用访问鉴权，无需登录');
          navigate(redirectTo, { replace: true });
          return;
        }
      } catch {
        if (!alive) return;
        setAuthRequired(true);
        message.warning('无法探测鉴权状态，若服务端已启用鉴权请填写 api_key 后登录');
      } finally {
        if (alive) setBooting(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [navigate, redirectTo]);

  const [submitting, setSubmitting] = useState(false);

  const onFinish = async (v: { api_key: string }) => {
    setSubmitting(true);
    try {
      await verifyAccessLogin(v.api_key);
      saveUnifiedApiKey(v.api_key);
      if (loadAccessCredentials()) {
        message.success('登录校验成功');
        navigate(redirectTo, { replace: true });
      }
    } catch (e: unknown) {
      const msg = e instanceof Error ? e.message : String(e);
      message.error(msg || '校验失败');
    } finally {
      setSubmitting(false);
    }
  };

  if (booting) {
    return (
      <div style={{ paddingTop: '20vh', textAlign: 'center' }}>
        <Spin size="large" tip="检查鉴权状态…" />
      </div>
    );
  }

  if (!authRequired) {
    return null;
  }

  return (
    <div style={{ maxWidth: 420, margin: '10vh auto', padding: '0 16px' }}>
      <Card title="管理端登录" bordered={false}>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 16 }}>
          服务端已启用鉴权。请填写配置的 <Typography.Text code>api_key</Typography.Text>{' '}
          中与某一段完全一致的一串密钥；系统将调用{' '}
          <Typography.Text code>POST /api/v1/auth/login</Typography.Text>{' '}
          校验。通过后写入 sessionStorage，后续 REST 使用{' '}
          <Typography.Text code>HEX(SHA1(k+&quot;\n&quot;+ts+&quot;\n&quot;+k))</Typography.Text>
          {' '}签名。
        </Typography.Paragraph>
        <Form layout="vertical" onFinish={onFinish} requiredMark={false}>
          <Form.Item
            label="API Key"
            name="api_key"
            rules={[{ required: true, message: '请输入与服务端任选片段一致的密钥' }]}
          >
            <Input
              prefix={<UserOutlined />}
              autoComplete="username"
              placeholder="例如 dev-key-one（与 server.auth.api_key 逗号分段之一一致）"
            />
          </Form.Item>
          <Form.Item style={{ marginBottom: 0 }}>
            <Button type="primary" htmlType="submit" block size="large" loading={submitting}>
              登录
            </Button>
          </Form.Item>
        </Form>
      </Card>
    </div>
  );
}
