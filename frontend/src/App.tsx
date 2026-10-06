import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouterState } from "@tanstack/react-router";
import { Alert, Button, Card, Form, Input, Spin, Typography } from "antd";
import { useEffect } from "react";

import {
  AUTHENTICATION_REQUIRED_EVENT,
  ApiError,
  changeTemporaryPassword,
  getCurrentUser,
  login,
  type CurrentUser,
} from "./api/client";
import { Workspace } from "./Workspace";

const { Paragraph, Text, Title } = Typography;

export function App() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const path = useRouterState({ select: (state) => state.location.pathname });
  const currentUser = useQuery({
    queryKey: ["current-user"],
    queryFn: getCurrentUser,
    retry: false,
    staleTime: Number.POSITIVE_INFINITY,
  });
  const acceptUser = (user: CurrentUser) => {
    queryClient.setQueryData(["current-user"], user);
    if (path === "/" || path === "/login") void navigate({ to: "/sessions" });
  };
  const loginMutation = useMutation({ mutationFn: login, onSuccess: acceptUser });
  const passwordMutation = useMutation({
    mutationFn: changeTemporaryPassword,
    onSuccess: acceptUser,
  });
  useEffect(() => {
    const requireAuthentication = () => {
      void queryClient.resetQueries({ exact: true, queryKey: ["current-user"] });
    };
    window.addEventListener(AUTHENTICATION_REQUIRED_EVENT, requireAuthentication);
    return () => {
      window.removeEventListener(AUTHENTICATION_REQUIRED_EVENT, requireAuthentication);
    };
  }, [queryClient]);

  if (currentUser.isPending) return <LoadingScreen />;
  if (currentUser.isError) {
    if (currentUser.error instanceof ApiError && currentUser.error.status === 401) {
      return (
        <LoginScreen
          error={loginMutation.isError}
          loading={loginMutation.isPending}
          submit={(values) => {
            loginMutation.mutate(values);
          }}
        />
      );
    }
    return <UnavailableScreen retry={() => void currentUser.refetch()} />;
  }
  if (currentUser.data.requiresPasswordChange) {
    return (
      <PasswordChangeScreen
        error={passwordMutation.isError}
        loading={passwordMutation.isPending}
        submit={(password) => {
          passwordMutation.mutate(password);
        }}
      />
    );
  }
  return <Workspace user={currentUser.data} />;
}

function LoadingScreen() {
  return (
    <main className="centered-screen" aria-busy="true">
      <Spin size="large" />
      <Text role="status">Открываем рабочее место...</Text>
    </main>
  );
}

type LoginValues = { login: string; password: string };

function LoginScreen({
  error,
  loading,
  submit,
}: {
  error: boolean;
  loading: boolean;
  submit: (values: LoginValues) => void;
}) {
  return (
    <main className="auth-screen">
      <section className="auth-intro" aria-label="DRIP Недвижимость">
        <div className="brand-mark" aria-hidden="true">
          DRIP
        </div>
        <Text className="brand-name">DRIP Недвижимость</Text>
        <Title level={1}>Аналитика недвижимости</Title>
        <Paragraph>
          Общее рабочее место для проверяемых аналитических сессий и рыночных данных.
        </Paragraph>
      </section>
      <Card className="auth-card" variant="borderless">
        <Text className="eyebrow">Внутренняя платформа</Text>
        <Title level={2}>Вход</Title>
        <Paragraph type="secondary">Используйте выданные вам локальные учетные данные.</Paragraph>
        {error ? (
          <Alert
            showIcon
            type="error"
            title="Не удалось войти"
            description="Имя входа или пароль неверны"
          />
        ) : null}
        <Form<LoginValues> layout="vertical" requiredMark={false} onFinish={submit}>
          <Form.Item label="Имя входа" name="login" rules={[{ required: true }]}>
            <Input
              autoComplete="username"
              autoFocus
              prefix={<UserOutlined aria-hidden="true" />}
              size="large"
            />
          </Form.Item>
          <Form.Item label="Пароль" name="password" rules={[{ required: true }]}>
            <Input.Password
              autoComplete="current-password"
              prefix={<LockOutlined aria-hidden="true" />}
              size="large"
            />
          </Form.Item>
          <Button block htmlType="submit" loading={loading} size="large" type="primary">
            Войти
          </Button>
        </Form>
      </Card>
    </main>
  );
}

type PasswordValues = { password: string; confirmation: string };

function PasswordChangeScreen({
  error,
  loading,
  submit,
}: {
  error: boolean;
  loading: boolean;
  submit: (password: string) => void;
}) {
  return (
    <main className="centered-screen password-screen">
      <Card className="password-card" variant="borderless">
        <Text className="eyebrow">Первый вход</Text>
        <Title level={1}>Создайте постоянный пароль</Title>
        <Paragraph type="secondary">
          Используйте не менее 15 символов. Можно использовать пробелы и парольную фразу.
        </Paragraph>
        {error ? <Alert showIcon type="error" title="Пароль не соответствует требованиям" /> : null}
        <Form<PasswordValues>
          layout="vertical"
          requiredMark={false}
          onFinish={({ password }) => {
            submit(password);
          }}
        >
          <Form.Item
            label="Новый пароль"
            name="password"
            rules={[{ required: true, min: 15, max: 128 }]}
          >
            <Input.Password autoComplete="new-password" size="large" />
          </Form.Item>
          <Form.Item
            dependencies={["password"]}
            label="Повторите новый пароль"
            name="confirmation"
            rules={[
              { required: true },
              ({ getFieldValue }) => ({
                validator(_, value) {
                  return value === getFieldValue("password")
                    ? Promise.resolve()
                    : Promise.reject(new Error("Пароли не совпадают"));
                },
              }),
            ]}
          >
            <Input.Password autoComplete="new-password" size="large" />
          </Form.Item>
          <Button block htmlType="submit" loading={loading} size="large" type="primary">
            Сохранить пароль
          </Button>
        </Form>
      </Card>
    </main>
  );
}

function UnavailableScreen({ retry }: { retry: () => void }) {
  return (
    <main className="centered-screen">
      <Alert
        action={<Button onClick={retry}>Повторить</Button>}
        description="Проверьте подключение и попробуйте еще раз."
        title="Рабочее место временно недоступно"
        showIcon
        type="error"
      />
    </main>
  );
}
