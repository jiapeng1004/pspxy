import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useLocation, useSearchParams } from 'react-router-dom';
import { Button, Card, Form, Input, Typography, message, Spin } from 'antd';
import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { fetchAuthRequired, verifyAccessLogin } from '../api/client';
import {
  saveAccessCredentials,
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
        message.warning('无法探测鉴权状态，若服务端已启用 AK/SK 请填写后登录');
      } finally {
        if (alive) setBooting(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, [navigate, redirectTo]);

  const [submitting, setSubmitting] = useState(false);

  const onFinish = async (v: { access_key: string; secret_key: string }) => {
    setSubmitting(true);
    try {
      await verifyAccessLogin(v.access_key, v.secret_key);
      saveAccessCredentials(v.access_key, v.secret_key);
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
          服务端已配置 AK/SK。提交后将调用{' '}
          <Typography.Text code>POST /api/v1/auth/login</Typography.Text> 校验凭据是否与配置一致；通过后密钥写入
          sessionStorage，后续 REST 请求将带签名头。
        </Typography.Paragraph>
        <Form layout="vertical" onFinish={onFinish} requiredMark={false}>
          <Form.Item
            label="Access Key"
            name="access_key"
            rules={[{ required: true, message: '请输入 Access Key' }]}
          >
            <Input
              prefix={<UserOutlined />}
              autoComplete="username"
              placeholder="与服务端配置的 access_key 一致"
            />
          </Form.Item>
          <Form.Item
            label="Secret Key"
            name="secret_key"
            rules={[{ required: true, message: '请输入 Secret Key' }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              autoComplete="current-password"
              placeholder="与服务端配置的 secret_key 一致"
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
