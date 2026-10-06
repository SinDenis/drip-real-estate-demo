import { useNavigate } from "@tanstack/react-router";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Form, Select, Spin, Tag, Typography } from "antd";
import { useMemo, useRef, useState } from "react";

import {
  createAnalyticalSession,
  listRegions,
  listResidentialComplexes,
  listSegments,
  type ResidentialComplexCatalog,
  type SegmentCatalog,
} from "./api/client";

const { Paragraph, Text, Title } = Typography;
type SegmentCode = SegmentCatalog["items"][number]["code"];

// eslint-disable-next-line complexity, max-lines-per-function -- The form keeps dependent selections and support feedback in one user-facing state machine.
export function CurrentMarketForm() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const regions = useQuery({ queryKey: ["regions"], queryFn: listRegions, retry: false });
  const segments = useQuery({ queryKey: ["segments"], queryFn: listSegments, retry: false });
  const [regionCodes, setRegionCodes] = useState<string[]>([]);
  const [complexIDs, setComplexIDs] = useState<string[]>([]);
  const [segmentCodes, setSegmentCodes] = useState<SegmentCode[]>([]);
  const pendingSubmissionKey = useRef<string | undefined>(undefined);
  const complexes = useQueries({
    queries: regionCodes.map((regionCode) => ({
      queryKey: ["residential-complexes", regionCode],
      queryFn: () => listResidentialComplexes(regionCode),
      retry: false,
    })),
  });
  const availableComplexes = useMemo(
    () =>
      complexes.flatMap((query, index) =>
        (query.data?.items ?? []).map((item) => ({ ...item, regionCode: regionCodes[index] })),
      ),
    [complexes, regionCodes],
  );
  const selectedSegments = (segments.data?.items ?? []).filter((segment) =>
    segmentCodes.includes(segment.code),
  );
  const supportedCount = selectedSegments.filter((segment) => segment.eiszhsSupported).length;
  const allUnsupported = selectedSegments.length > 0 && supportedCount === 0;
  const mixedSupport = supportedCount > 0 && supportedCount < selectedSegments.length;
  const ready =
    regionCodes.length > 0 && complexIDs.length > 0 && segmentCodes.length > 0 && !allUnsupported;
  const creation = useMutation({
    mutationFn: ({
      input,
      idempotencyKey,
    }: {
      input: Parameters<typeof createAnalyticalSession>[0];
      idempotencyKey: string;
    }) => createAnalyticalSession(input, idempotencyKey),
    onSuccess: (session) => {
      pendingSubmissionKey.current = undefined;
      void queryClient.invalidateQueries({ queryKey: ["analytical-sessions"] });
      void navigate({ to: "/sessions/$sessionId", params: { sessionId: session.id } });
    },
  });

  if (regions.isPending || segments.isPending) {
    return <Spin description="Загружаем справочники..." size="large" />;
  }
  if (regions.isError || segments.isError) {
    return (
      <Alert
        action={
          <Button
            onClick={() => {
              void regions.refetch();
              void segments.refetch();
            }}
          >
            Повторить
          </Button>
        }
        role="alert"
        title="Не удалось загрузить справочники"
        type="error"
      />
    );
  }

  const changeRegions = (next: string[]) => {
    pendingSubmissionKey.current = undefined;
    const retainedComplexIDs = new Set(
      availableComplexes
        .filter((item) => next.includes(item.regionCode))
        .map((item) => item.complexId),
    );
    setRegionCodes(next);
    setComplexIDs((current) => current.filter((id) => retainedComplexIDs.has(id)));
  };

  const changeComplexes = (next: string[]) => {
    pendingSubmissionKey.current = undefined;
    setComplexIDs(next);
  };

  const changeSegments = (next: SegmentCode[]) => {
    pendingSubmissionKey.current = undefined;
    setSegmentCodes(next);
  };

  return (
    <section className="current-market-form" aria-labelledby="current-market-heading">
      <Text className="eyebrow">Новая аналитическая сессия</Text>
      <Title id="current-market-heading" level={1}>
        Выберите текущий рынок
      </Title>
      <Paragraph type="secondary">
        Выберите регионы, ЖК и сегменты. Сессия будет использовать только новые данные текущего
        месяца.
      </Paragraph>
      <Form
        layout="vertical"
        onFinish={() => {
          if (!ready || creation.isPending) return;
          pendingSubmissionKey.current ??= newIdempotencyKey();
          creation.mutate({
            input: {
              regionCodes: [...regionCodes],
              residentialComplexIds: [...complexIDs],
              segmentCodes: [...segmentCodes],
            },
            idempotencyKey: pendingSubmissionKey.current,
          });
        }}
        requiredMark={false}
      >
        <Form.Item label="Регионы" required>
          <Select
            aria-label="Регионы"
            mode="multiple"
            onChange={changeRegions}
            options={regions.data.items.map((region) => ({
              label: region.name,
              value: region.code,
            }))}
            placeholder="Выберите один или несколько регионов"
            showSearch={{ optionFilterProp: "label" }}
            value={regionCodes}
          />
        </Form.Item>
        <Form.Item
          extra="Можно выбрать от одного до двадцати ЖК только из выбранных регионов"
          label="Жилые комплексы"
          required
        >
          <Select
            aria-label="Жилые комплексы"
            disabled={regionCodes.length === 0}
            loading={complexes.some((query) => query.isPending)}
            maxCount={20}
            mode="multiple"
            onChange={changeComplexes}
            options={availableComplexes.map((item) => ({
              label: item.name,
              value: item.complexId,
            }))}
            placeholder="Найдите ЖК по названию"
            showSearch={{ optionFilterProp: "label" }}
            value={complexIDs}
          />
        </Form.Item>
        <ComplexCatalogState catalogs={complexes.map((query) => query.data)} retry={complexes} />
        <Form.Item label="Сегменты недвижимости" required>
          <Select
            aria-label="Сегменты недвижимости"
            mode="multiple"
            onChange={changeSegments}
            options={segmentOptions(segments.data)}
            placeholder="Выберите один или несколько сегментов"
            showSearch={{ optionFilterProp: "searchLabel" }}
            value={segmentCodes}
          />
        </Form.Item>
        {allUnsupported ? (
          <Alert
            role="alert"
            title="Выбранные сегменты не поддерживаются ЕИСЖС"
            description="Добавьте хотя бы один поддерживаемый сегмент."
            type="error"
          />
        ) : null}
        {mixedSupport ? (
          <Alert
            role="status"
            title="Часть сегментов пока не поддерживается ЕИСЖС"
            description="Сессия будет создана с предупреждением о частичном покрытии."
            type="warning"
          />
        ) : null}
        {creation.isError ? (
          <Alert
            role="alert"
            title="Не удалось создать аналитическую сессию"
            description={creation.error.message}
            type="error"
          />
        ) : null}
        <div className="current-market-actions">
          <Button
            disabled={!ready}
            htmlType="submit"
            loading={creation.isPending}
            size="large"
            type="primary"
          >
            Создать сессию
          </Button>
          <Text type="secondary">После создания сразу откроется сохраненный дашборд.</Text>
        </div>
      </Form>
    </section>
  );
}

function newIdempotencyKey() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

function segmentOptions(catalog: SegmentCatalog) {
  return catalog.items.map((segment) => ({
    value: segment.code,
    searchLabel: segment.name,
    label: (
      <span className="segment-option">
        <span>{segment.name}</span>
        <Tag color={segment.eiszhsSupported ? "green" : "default"}>
          {segment.eiszhsSupported ? "Поддерживается ЕИСЖС" : "Не поддерживается ЕИСЖС"}
        </Tag>
      </span>
    ),
  }));
}

function ComplexCatalogState({
  catalogs,
  retry,
}: {
  catalogs: Array<ResidentialComplexCatalog | undefined>;
  retry: Array<{ isError: boolean; refetch: () => Promise<unknown> }>;
}) {
  const stale = catalogs.find((catalog) => catalog?.freshness === "stale");
  const failed = retry.filter((query) => query.isError);
  if (failed.length > 0) {
    return (
      <Alert
        action={
          <Button
            onClick={() => {
              failed.forEach((query) => void query.refetch());
            }}
          >
            Повторить
          </Button>
        }
        role="alert"
        title="Справочник ЖК временно недоступен"
        type="error"
      />
    );
  }
  if (!stale) return null;
  return (
    <Alert
      role="status"
      title={stale.warning ?? "Показан устаревший справочник ЖК"}
      description={`Получен ${formatTimestamp(stale.observedAt)}, истек ${formatTimestamp(stale.expiresAt)}`}
      type="warning"
    />
  );
}

function formatTimestamp(value: string) {
  return new Intl.DateTimeFormat("ru-RU", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}
