import { MenuOutlined } from "@ant-design/icons";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { Button, Drawer, Layout, Typography } from "antd";
import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";

import { type CurrentUser } from "./api/client";
import { EmployeesScreen } from "./EmployeesScreen";
import { SessionOverview } from "./SessionOverview";
import { CurrentMarketForm } from "./CurrentMarketForm";
import { SessionSidebar } from "./SessionSidebar";

const { Content, Sider } = Layout;
const { Text } = Typography;

export function Workspace({ user }: { user: CurrentUser }) {
  const path = useRouterState({ select: (state) => state.location.pathname });
  const navigate = useNavigate();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [collapsed, setCollapsed] = useState(
    () => localStorage.getItem("sidebar-collapsed") === "true",
  );
  const isMobile = useMediaQuery("(max-width: 760px)");
  const closeDrawer = setDrawerOpen.bind(undefined, false);
  const selectedSessionId =
    path.startsWith("/sessions/") && path !== "/sessions/new"
      ? path.slice("/sessions/".length)
      : undefined;
  const toggleCollapsed = () => {
    setCollapsed((current) => {
      localStorage.setItem("sidebar-collapsed", String(!current));
      return !current;
    });
  };
  const selectSession = (sessionId: string) => {
    setDrawerOpen(false);
    void navigate({ to: "/sessions/$sessionId", params: { sessionId } });
  };
  const sidebar = (
    <SessionSidebar
      collapsed={collapsed && !isMobile}
      onCollapse={toggleCollapsed}
      onNew={() => {
        setDrawerOpen(false);
        void navigate({ to: "/sessions/new" });
      }}
      onSelect={selectSession}
      selectedSessionId={selectedSessionId}
      user={user}
    />
  );

  return (
    <Layout className="application-shell">
      <AdaptiveSidebar
        collapsed={collapsed}
        drawerOpen={drawerOpen}
        isMobile={isMobile}
        onClose={closeDrawer}
        sidebar={sidebar}
      />
      <Layout>
        {isMobile ? (
          <MobileToolbar
            open={() => {
              setDrawerOpen(true);
            }}
          />
        ) : null}
        <Content className="workspace-content">
          <WorkspaceContent path={path} sessionId={selectedSessionId} />
        </Content>
      </Layout>
    </Layout>
  );
}

function AdaptiveSidebar({
  collapsed,
  drawerOpen,
  isMobile,
  onClose,
  sidebar,
}: {
  collapsed: boolean;
  drawerOpen: boolean;
  isMobile: boolean;
  onClose: () => void;
  sidebar: ReactNode;
}) {
  if (!isMobile) {
    return (
      <Sider
        className="session-sidebar"
        collapsed={collapsed}
        collapsedWidth={72}
        theme="light"
        trigger={null}
        width={304}
      >
        {sidebar}
      </Sider>
    );
  }
  return (
    <Drawer
      className="mobile-session-drawer"
      closeIcon={null}
      onClose={onClose}
      open={drawerOpen}
      placement="left"
      size={304}
      title={null}
    >
      {sidebar}
    </Drawer>
  );
}

function MobileToolbar({ open }: { open: () => void }) {
  return (
    <header className="mobile-toolbar">
      <Button
        aria-label="Открыть список аналитических сессий"
        icon={<MenuOutlined />}
        onClick={open}
        type="text"
      />
      <Text strong>DRIP Недвижимость</Text>
    </header>
  );
}

function WorkspaceContent({ path, sessionId }: { path: string; sessionId?: string }) {
  if (path === "/employees") return <EmployeesScreen />;
  if (path === "/sessions/new") return <CurrentMarketForm />;
  if (sessionId) return <SessionOverview sessionId={sessionId} />;
  return <WorkspaceEmpty />;
}

function WorkspaceEmpty() {
  return (
    <section className="workspace-empty" aria-labelledby="workspace-heading">
      <Text className="eyebrow">Общее рабочее место</Text>
      <Typography.Title id="workspace-heading" level={2}>
        Выберите аналитическую сессию
      </Typography.Title>
      <Typography.Paragraph type="secondary">
        Здесь появятся сохраненные фильтры, статус и результаты выбранного анализа.
      </Typography.Paragraph>
    </section>
  );
}

function useMediaQuery(query: string) {
  const media = useMemo(() => window.matchMedia(query), [query]);
  const [matches, setMatches] = useState(media.matches);
  useEffect(() => {
    const listener = () => {
      setMatches(media.matches);
    };
    media.addEventListener("change", listener);
    return () => {
      media.removeEventListener("change", listener);
    };
  }, [media]);
  return matches;
}
