import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Progress,
  Spin,
  Statistic,
  Tag,
  Typography,
} from "antd";
import { useEffect } from "react";

import { ApiError, collectCurrentSnapshot, getAnalyticalSession } from "./api/client";

const { Paragraph, Text, Title } = Typography;

export function SessionOverview({ sessionId }: { sessionId: string }) {
  const queryClient = useQueryClient();
  const session = useQuery({
    queryKey: ["analytical-session", sessionId],
    queryFn: () => getAnalyticalSession(sessionId),
    retry: false,
  });
  const collection = useMutation({
    mutationFn: () => collectCurrentSnapshot(sessionId),
    onSuccess: (updated) => {
      queryClient.setQueryData(["analytical-session", sessionId], updated);
      void queryClient.invalidateQueries({ queryKey: ["analytical-sessions"] });
    },
  });
  const completedRequests = session.data?.collectionProgress?.completedRequests;
  useEffect(() => {
    if (session.data?.status === "collecting" && !collection.isPending && !collection.isError) {
      collection.mutate();
    }
    // completedRequests is a durable checkpoint token that advances the next sequential call.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session.data?.status, completedRequests]);
  if (session.isPending) return <Spin description="Открываем сессию..." size="large" />;
  if (session.isError) {
    const missing = session.error instanceof ApiError && session.error.status === 404;
    return (
      <section className="state-panel">
        <Empty
          description={missing ? "Сессия не найдена" : "Не удалось открыть аналитическую сессию"}
        />
      </section>
    );
  }
  return (
    <article className="session-overview">
      <header className="overview-header">
        <div>
          <Text className="eyebrow">Обзор аналитической сессии</Text>
          <Title level={1}>{session.data.name}</Title>
          <Paragraph type="secondary">{session.data.subtitle}</Paragraph>
        </div>
        <StatusTag status={session.data.status} />
      </header>
      <CollectionState
        error={collection.isError}
        onRetry={() => {
          collection.reset();
          collection.mutate();
        }}
        session={session.data}
      />
      <CurrentDashboard session={session.data} />
      <SessionFilters session={session.data} />
    </article>
  );
}

function CollectionState({
  error,
  onRetry,
  session,
}: {
  error: boolean;
  onRetry: () => void;
  session: Awaited<ReturnType<typeof getAnalyticalSession>>;
}) {
  const progress = session.collectionProgress;
  if (!progress) return null;
  const totalRequests = progress.totalRequests;
  const planning = totalRequests == null;
  const percent = collectionPercent(progress);
  return (
    <section className="collection-state" aria-label="Прогресс получения данных ЕИСЖС">
      <div className="collection-copy">
        <Text strong>
          {planning
            ? `${String(progress.completedRequests)} запросов ЕИСЖС - выполняется планирование`
            : `${String(progress.completedRequests)} из ${String(totalRequests)} запросов ЕИСЖС`}
        </Text>
        <Text type="secondary">Каждый ответ сохраняется до расчета показателей</Text>
      </div>
      {planning ? null : (
        <Progress percent={percent} showInfo={false} status={error ? "exception" : undefined} />
      )}
      <CollectionProgressDetails progress={progress} />
      <CollectionTarget progress={progress} />
      {error ? (
        <Alert
          action={<Button onClick={onRetry}>Повторить</Button>}
          role="alert"
          title="Не удалось продолжить получение данных ЕИСЖС"
          type="error"
        />
      ) : null}
    </section>
  );
}

type CollectionProgressData = NonNullable<
  Awaited<ReturnType<typeof getAnalyticalSession>>["collectionProgress"]
>;

function collectionPercent(progress: CollectionProgressData) {
  if (progress.percent != null) return progress.percent;
  if (progress.totalRequests == null || progress.totalRequests === 0) return 0;
  return Math.round((progress.completedRequests / progress.totalRequests) * 100);
}

function CollectionProgressDetails({ progress }: { progress: CollectionProgressData }) {
  return (
    <div className="collection-details" aria-label="Подробный прогресс сбора">
      <Text>Найдено объектов: {progress.discoveredTotal}</Text>
      <Text>Из кэша: {progress.cached}</Text>
      <Text>Получено: {progress.fetched}</Text>
      <Text>Ошибок: {progress.failed}</Text>
      {progress.remaining == null ? null : <Text>Осталось запросов: {progress.remaining}</Text>}
    </div>
  );
}

function CollectionTarget({ progress }: { progress: CollectionProgressData }) {
  if (progress.currentObjectId) {
    return <Text type="secondary">Текущий объект ЕИСЖС: {progress.currentObjectId}</Text>;
  }
  if (progress.currentComplexId) {
    return <Text type="secondary">Текущий ЖК ЕИСЖС: {progress.currentComplexId}</Text>;
  }
  return null;
}

function CurrentDashboard({
  session,
}: {
  session: Awaited<ReturnType<typeof getAnalyticalSession>>;
}) {
  const dashboard = session.currentEiszhsDashboard;
  if (!dashboard) return null;
  return (
    <section className="current-dashboard" aria-label={dashboard.sourceLabel}>
      <div>
        <Text className="eyebrow">Источник показателя</Text>
        <Title level={2}>{dashboard.sourceLabel}</Title>
      </div>
      <Card className="metric-card">
        <Statistic
          title="Накопительное количество ДДУ"
          value={dashboard.cumulativeDduCount ?? "Нет данных"}
        />
        <Text type="secondary">Опубликованное текущее значение, не продажи месяца</Text>
      </Card>
    </section>
  );
}

function SessionFilters({
  session,
}: {
  session: Awaited<ReturnType<typeof getAnalyticalSession>>;
}) {
  const filters = session.filters;
  return (
    <Descriptions bordered column={{ xs: 1, md: 2 }} size="small" title="Канонические фильтры">
      <Descriptions.Item label="Регионы">{filters.regions.join(", ")}</Descriptions.Item>
      <Descriptions.Item label="Жилые комплексы">
        {filters.residentialComplexes.join(", ")}
      </Descriptions.Item>
      <Descriptions.Item label="Сегменты">{filters.segments.join(", ")}</Descriptions.Item>
      <Descriptions.Item label="Фактическое окно наблюдения">
        {session.observedWindow ? (
          <>
            {formatTimestamp(session.observedWindow.from)} -{" "}
            {formatTimestamp(session.observedWindow.to)}
          </>
        ) : (
          <Text type="secondary">Данные текущего месяца еще не получены</Text>
        )}
      </Descriptions.Item>
      <Descriptions.Item label="Создал">{session.createdBy.displayName}</Descriptions.Item>
      <Descriptions.Item label="Создана">{formatDate(session.createdAt)}</Descriptions.Item>
    </Descriptions>
  );
}

function StatusTag({ status }: { status: string }) {
  const labels: Record<string, string> = {
    queued: "В очереди",
    collecting: "Собираем данные",
    complete: "Готово",
    partial: "Частично",
    failed: "Ошибка",
    canceled: "Отменена",
  };
  const colors: Record<string, string> = {
    queued: "blue",
    collecting: "processing",
    complete: "success",
    partial: "warning",
    failed: "error",
    canceled: "default",
  };
  return <Tag color={colors[status]}>{labels[status] ?? status}</Tag>;
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit",
    month: "short",
    year: "numeric",
  }).format(new Date(value));
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("ru-RU", {
    dateStyle: "short",
    timeStyle: "short",
  }).format(new Date(value));
}
