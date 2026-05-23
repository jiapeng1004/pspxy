import { useCallback, useEffect, useState } from 'react';
import { Layout, Menu, Typography, theme } from 'antd';
import { SwapOutlined } from '@ant-design/icons';
import type { MenuProps } from 'antd';
import type { MultiTunnelStatus } from './api/tunnel';
import { fetchTunnelStatus } from './api/tunnel';
import ForwardTunnelPanel from './ForwardTunnelPanel';
import ReverseTunnelPanel from './ReverseTunnelPanel';

type NavKey = 'forward' | 'reverse';

export default function App() {
  const [status, setStatus] = useState<MultiTunnelStatus | null>(null);
  const [nav, setNav] = useState<NavKey>('forward');
  const { token } = theme.useToken();

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
    return () => window.clearInterval(id);
  }, [poll]);

  const menuItems: MenuProps['items'] = [
    { key: 'forward', icon: <SwapOutlined rotate={90} />, label: '隧道穿透' },
    { key: 'reverse', icon: <SwapOutlined />, label: '出站暴露' },
  ];

  return (
    <Layout style={{ minHeight: '100vh', background: token.colorBgLayout }}>
      <Layout.Sider
        width={216}
        style={{
          paddingTop: 20,
          background: '#fff',
          borderRight: `1px solid ${token.colorBorderSecondary}`,
          boxShadow: '2px 0 12px rgba(0,0,0,.04)',
        }}
      >
        <div style={{ padding: '8px 20px 20px', fontWeight: 600, fontSize: 15 }}>
          TCP Proxy 客户端
        </div>
        <Menu
          mode="inline"
          selectedKeys={[nav]}
          items={menuItems}
          onClick={(e) => setNav(e.key as NavKey)}
          style={{ borderInlineEnd: 'none', fontWeight: 500 }}
        />
      </Layout.Sider>

      <Layout style={{ padding: '24px 32px', background: 'transparent' }}>
        <Typography.Title level={3} style={{ marginTop: 0 }}>
          {nav === 'forward' ? '隧道穿透（订阅）' : '出站暴露（把本机端口挂到服务端）'}
        </Typography.Title>

        <div style={{ maxWidth: 1160 }}>
          {nav === 'forward' ? (
            <ForwardTunnelPanel status={status} onReplaceStatus={(s) => setStatus(s)} />
          ) : (
            <ReverseTunnelPanel status={status} onReplaceStatus={(s) => setStatus(s)} />
          )}
        </div>
      </Layout>
    </Layout>
  );
}
