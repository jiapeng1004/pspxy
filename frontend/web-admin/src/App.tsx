import { useEffect, useState } from 'react';
import {
  Routes,
  Route,
  Navigate,
  useNavigate,
  useLocation,
} from 'react-router-dom';
import { Layout, Menu, theme, Spin, Button, Space } from 'antd';
import { ApiOutlined, LogoutOutlined, SettingOutlined } from '@ant-design/icons';
import ProxyList from './pages/ProxyList';
import ProxyForm from './pages/ProxyForm';
import LoginPage from './pages/Login';
import { fetchAuthRequired } from './api/client';
import {
  clearAccessCredentials,
  loadAccessCredentials,
} from './auth/session';

const { Header, Sider, Content } = Layout;

const menuItems = [
  { key: '/proxies', icon: <ApiOutlined />, label: '代理管理' },
  { key: '/settings', icon: <SettingOutlined />, label: '系统设置' },
];

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="*" element={<MainWorkspace />} />
    </Routes>
  );
}

function MainWorkspace() {
  const navigate = useNavigate();
  const location = useLocation();
  const [boot, setBoot] = useState({ loading: true, authRequired: false });

  const {
    token: { colorBgContainer },
  } = theme.useToken();

  useEffect(() => {
    let alive = true;
    (async () => {
      try {
        const required = await fetchAuthRequired();
        if (alive) setBoot({ loading: false, authRequired: required });
      } catch {
        if (alive) setBoot({ loading: false, authRequired: false });
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  if (boot.loading) {
    return (
      <div style={{ paddingTop: '25vh', textAlign: 'center' }}>
        <Spin size="large" tip="正在连接管理服务…" />
      </div>
    );
  }

  if (boot.authRequired && !loadAccessCredentials()) {
    const from = `${location.pathname}${location.search}`;
    return (
      <Navigate to="/login" replace state={{ from }} />
    );
  }

  const hasSessionCred = !!loadAccessCredentials();

  const logout = () => {
    clearAccessCredentials();
    navigate('/login', { replace: true });
  };

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider breakpoint="lg" collapsedWidth="0">
        <div
          style={{
            height: 32,
            margin: 16,
            color: '#fff',
            fontSize: 18,
            textAlign: 'center',
            fontWeight: 'bold',
          }}
        >
          TCP Proxy
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[location.pathname]}
          items={menuItems}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            padding: '0 24px',
            background: colorBgContainer,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
          }}
        >
          <span style={{ fontSize: 18, fontWeight: 500 }}>
            TCP 代理管理系统（管理端）
          </span>
          <Space>
            {boot.authRequired && hasSessionCred ? (
              <Button icon={<LogoutOutlined />} onClick={logout}>
                退出登录
              </Button>
            ) : null}
          </Space>
        </Header>
        <Content style={{ margin: 24 }}>
          <div
            style={{
              padding: 24,
              background: colorBgContainer,
              borderRadius: 8,
              minHeight: 360,
            }}
          >
            <Routes>
              <Route path="/proxies" element={<ProxyList />} />
              <Route path="/proxies/new" element={<ProxyForm />} />
              <Route path="/proxies/:id/edit" element={<ProxyForm />} />
              <Route path="/settings" element={<SettingsPlaceholder />} />
              <Route path="*" element={<Navigate to="/proxies" replace />} />
            </Routes>
          </div>
        </Content>
      </Layout>
    </Layout>
  );
}

function SettingsPlaceholder() {
  return (
    <div style={{ textAlign: 'center', padding: 40, color: '#999' }}>
      系统设置页面（建设中）
    </div>
  );
}
