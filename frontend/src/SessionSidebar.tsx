import {
  LeftOutlined,
  LogoutOutlined,
  PlusOutlined,
  RightOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { Alert, Button, Dropdown, Spin, Typography } from "antd";

import {
  listAnalyticalSessions,
  logout,
  type AnalyticalSessionPage,
  type CurrentUser,
} from "./api/client";

const { Text, Title } = Typography;
const activeStatuses = new Set(["queued", "collecting"]);
type SessionSummary = AnalyticalSessionPage["items"][number];

export function SessionSidebar({
  collapsed,
  onCollapse,
  onNew,
  onSelect,
  selectedSessionId,
  user,
}: {
  collapsed: boolean;
  onCollapse: () => void;
  onNew: () => void;
  onSelect: (id: string) => void;
  selectedSessionId?: string;
  user: CurrentUser;
}) {
  const sessions = useInfiniteQuery({
    queryKey: ["analytical-sessions"],
    queryFn: ({ pageParam }) => listAnalyticalSessions(pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
    retry: false,
  });
  const items = sessions.data?.pages.flatMap((page) => page.items) ?? [];
  const active = items.filter((session) => activeStatuses.has(session.status));
  const recent = items.filter((session) => !activeStatuses.has(session.status));
  return (
    <div className="sidebar-grid">
      <SidebarBrand collapsed={collapsed} />
      <div className="sidebar-title-row">
        {!collapsed ? <Title level={1}>Аналитические сессии</Title> : null}
        <SidebarTitleActions collapsed={collapsed} onCollapse={onCollapse} onNew={onNew} />
      </div>
      <SessionNavigation
        active={active}
        collapsed={collapsed}
        error={sessions.isError}
        hasNextPage={sessions.hasNextPage}
        loading={sessions.isPending}
        loadingNext={sessions.isFetchingNextPage}
        loadNext={() => {
          void sessions.fetchNextPage();
        }}
        onSelect={onSelect}
        recent={recent}
        selectedSessionId={selectedSessionId}
      />
      <ProfileMenu collapsed={collapsed} user={user} />
    </div>
  );
}

function SidebarTitleActions({
  collapsed,
  onCollapse,
  onNew,
}: {
  collapsed: boolean;
  onCollapse: () => void;
  onNew: () => void;
}) {
  return (
    <div className="sidebar-title-actions">
      <Button
        aria-label="Создать аналитическую сессию"
        icon={<PlusOutlined />}
        onClick={onNew}
        size="small"
        type="primary"
      />
      <Button
        aria-label={collapsed ? "Развернуть боковую панель" : "Свернуть боковую панель"}
        icon={collapsed ? <RightOutlined /> : <LeftOutlined />}
        onClick={onCollapse}
        size="small"
        type="text"
      />
    </div>
  );
}

function SidebarBrand({ collapsed }: { collapsed: boolean }) {
  return (
    <header className="sidebar-header">
      <div className="brand-mark brand-mark-small" aria-hidden="true">
        DRIP
      </div>
      {!collapsed ? (
        <div>
          <Text strong>DRIP Недвижимость</Text>
          <Text className="sidebar-caption">Аналитика рынка</Text>
        </div>
      ) : null}
    </header>
  );
}

function SessionNavigation({
  active,
  collapsed,
  error,
  hasNextPage,
  loading,
  loadingNext,
  loadNext,
  onSelect,
  recent,
  selectedSessionId,
}: {
  active: SessionSummary[];
  collapsed: boolean;
  error: boolean;
  hasNextPage: boolean;
  loading: boolean;
  loadingNext: boolean;
  loadNext: () => void;
  onSelect: (id: string) => void;
  recent: SessionSummary[];
  selectedSessionId?: string;
}) {
  return (
    <nav className="session-list" aria-label="Общие аналитические сессии">
      {loading ? <Spin description="Загружаем сессии..." size="small" /> : null}
      {error ? <Alert type="error" title="Не удалось загрузить сессии" /> : null}
      {!loading && active.length + recent.length === 0 && !collapsed ? <SidebarEmpty /> : null}
      <SessionGroup
        collapsed={collapsed}
        items={active}
        label="Активные"
        onSelect={onSelect}
        selectedSessionId={selectedSessionId}
      />
      <SessionGroup
        collapsed={collapsed}
        items={recent}
        label="Недавние"
        onSelect={onSelect}
        selectedSessionId={selectedSessionId}
      />
      {hasNextPage && !collapsed ? (
        <Button block loading={loadingNext} onClick={loadNext}>
          Показать еще
        </Button>
      ) : null}
    </nav>
  );
}

function SidebarEmpty() {
  return (
    <div className="sidebar-empty">
      <Text>Пока нет аналитических сессий</Text>
      <Text type="secondary">Новые сессии появятся здесь после отправки запроса.</Text>
    </div>
  );
}

function SessionGroup({
  collapsed,
  items,
  label,
  onSelect,
  selectedSessionId,
}: {
  collapsed: boolean;
  items: SessionSummary[];
  label: string;
  onSelect: (id: string) => void;
  selectedSessionId?: string;
}) {
  if (items.length === 0) return null;
  return (
    <section className="session-group" aria-label={label}>
      {!collapsed ? <Text className="session-group-title">{label}</Text> : null}
      {items.map((session) => (
        <button
          aria-current={selectedSessionId === session.id ? "page" : undefined}
          className="session-card"
          key={session.id}
          onClick={() => {
            onSelect(session.id);
          }}
          title={collapsed ? session.name : undefined}
          type="button"
        >
          <span className={`status-dot status-${session.status}`} aria-hidden="true" />
          {!collapsed ? <SessionCardCopy session={session} /> : null}
        </button>
      ))}
    </section>
  );
}

function SessionCardCopy({ session }: { session: SessionSummary }) {
  return (
    <span className="session-card-copy">
      <span className="session-name">{session.name}</span>
      <span className="session-subtitle">{session.subtitle}</span>
      <span className="session-meta">
        {formatDate(session.createdAt)} · {session.createdBy.displayName}
      </span>
    </span>
  );
}

function ProfileMenu({ collapsed, user }: { collapsed: boolean; user: CurrentUser }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const items = [
    ...(user.role === "analytics_admin"
      ? [{ key: "employees", icon: <TeamOutlined />, label: "Сотрудники" }]
      : []),
    { key: "logout", icon: <LogoutOutlined />, label: "Выйти" },
  ];
  const select = (key: string) => {
    if (key === "employees") {
      void navigate({ to: "/employees" });
      return;
    }
    void logout().finally(() => {
      queryClient.clear();
      window.location.assign("/login");
    });
  };
  return (
    <Dropdown
      menu={{
        items,
        onClick: ({ key }) => {
          select(key);
        },
      }}
      placement="topLeft"
      trigger={["click"]}
    >
      <Button aria-label="Открыть меню профиля" className="profile-summary" type="text">
        <span className="profile-avatar" aria-hidden="true">
          {user.displayName.slice(0, 1).toUpperCase()}
        </span>
        {!collapsed ? (
          <span className="profile-copy">
            <Text strong>{user.displayName}</Text>
            <Text className="sidebar-caption">
              {user.role === "analytics_admin" ? "Аналитик-администратор" : "Сотрудник"}
            </Text>
          </span>
        ) : null}
      </Button>
    </Dropdown>
  );
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  }).format(new Date(value));
}
