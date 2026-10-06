import { CopyOutlined, PlusOutlined } from "@ant-design/icons";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert, Button, Form, Input, Modal, Select, Space, Table, Typography } from "antd";
import { useEffect, useState } from "react";

import {
  ApiError,
  createEmployee,
  deactivateEmployee,
  listEmployees,
  resetEmployeePassword,
  restoreEmployee,
  revokeEmployeeSessions,
  updateEmployee,
  type Employee,
  type Role,
  type TemporaryCredential,
} from "./api/client";
import { AccountStateTag } from "./EmployeeStateTag";

const { Paragraph, Text, Title } = Typography;
type EmployeeFormValues = { login: string; displayName: string; role: Role };
type EmployeeAction = "deactivate" | "restore" | "reset" | "revoke";

export function EmployeesScreen() {
  const admin = useEmployeeAdmin();
  return (
    <section className="employees-screen">
      <header className="screen-header">
        <div>
          <Text className="eyebrow">Управление доступом</Text>
          <Title level={1}>Сотрудники</Title>
          <Paragraph type="secondary">Локальные учетные записи DRIP Недвижимость</Paragraph>
        </div>
        <Button
          icon={<PlusOutlined aria-hidden="true" />}
          onClick={admin.openCreate}
          type="primary"
        >
          Добавить сотрудника
        </Button>
      </header>
      <EmployeeErrors listError={admin.listError} mutationError={admin.actionError} />
      <Table<Employee>
        columns={employeeColumns(admin.edit, admin.runAction)}
        dataSource={admin.employees}
        loading={admin.loading}
        pagination={false}
        rowKey="id"
        scroll={{ x: 760 }}
        size="small"
      />
      <EmployeeModal
        confirmLoading={admin.saving}
        employee={admin.editing}
        form={admin.form}
        onCancel={admin.closeEditor}
        onFinish={admin.save}
        open={admin.editorOpen}
      />
      <CredentialModal credential={admin.credential} onClose={admin.closeCredential} />
    </section>
  );
}

function useEmployeeAdmin() {
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<Employee>();
  const [credential, setCredential] = useState<TemporaryCredential>();
  const [form] = Form.useForm<EmployeeFormValues>();
  const employees = useQuery({ queryKey: ["employees"], queryFn: listEmployees, retry: false });
  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ["employees"] });
  };
  const closeEditor = () => {
    setCreateOpen(false);
    setEditing(undefined);
    form.resetFields();
  };
  const created = (result: TemporaryCredential) => {
    closeEditor();
    setCredential(result);
    refresh();
  };
  const createMutation = useMutation({ mutationFn: createEmployee, onSuccess: created });
  const updateMutation = useUpdateEmployee(closeEditor, refresh);
  const actionMutation = useEmployeeAction(refresh, setCredential);
  const save = (values: EmployeeFormValues) => {
    if (editing) {
      updateMutation.mutate({ id: editing.id, displayName: values.displayName, role: values.role });
    } else {
      createMutation.mutate(values);
    }
  };
  return {
    actionError: actionMutation.error,
    closeCredential: () => {
      setCredential(undefined);
    },
    closeEditor,
    credential,
    edit: (employee: Employee) => {
      setEditing(employee);
    },
    editing,
    editorOpen: createOpen || Boolean(editing),
    employees: employees.data?.items ?? [],
    form,
    listError: employees.isError,
    loading: employees.isPending,
    openCreate: () => {
      setCreateOpen(true);
    },
    runAction: (employee: Employee, action: EmployeeAction) => {
      actionMutation.mutate({ employee, action });
    },
    save,
    saving: createMutation.isPending || updateMutation.isPending,
  };
}

function useUpdateEmployee(closeEditor: () => void, refresh: () => void) {
  return useMutation({
    mutationFn: updateEmployee,
    onSuccess: () => {
      closeEditor();
      refresh();
    },
  });
}

function useEmployeeAction(
  refresh: () => void,
  setCredential: (credential: TemporaryCredential) => void,
) {
  return useMutation({
    mutationFn: runEmployeeAction,
    onSuccess: (result) => {
      if (result && "temporaryPassword" in result) setCredential(result);
      refresh();
    },
  });
}

async function runEmployeeAction({
  employee,
  action,
}: {
  employee: Employee;
  action: EmployeeAction;
}) {
  if (action === "deactivate") return deactivateEmployee(employee.id);
  if (action === "restore") return restoreEmployee(employee.id);
  if (action === "reset") return resetEmployeePassword(employee.id);
  await revokeEmployeeSessions(employee.id);
  return undefined;
}

function EmployeeErrors({
  listError,
  mutationError,
}: {
  listError: boolean;
  mutationError: Error | null;
}) {
  if (listError) return <Alert type="error" title="Не удалось загрузить сотрудников" />;
  if (!mutationError) return null;
  const conflict = mutationError instanceof ApiError && mutationError.status === 409;
  return (
    <Alert
      closable
      title={
        conflict
          ? "Нельзя изменить последнего активного администратора"
          : "Не удалось изменить доступ сотрудника"
      }
      showIcon
      type="error"
    />
  );
}

function EmployeeModal({
  confirmLoading,
  employee,
  form,
  onCancel,
  onFinish,
  open,
}: {
  confirmLoading: boolean;
  employee?: Employee;
  form: ReturnType<typeof Form.useForm<EmployeeFormValues>>[0];
  onCancel: () => void;
  onFinish: (values: EmployeeFormValues) => void;
  open: boolean;
}) {
  useEffect(() => {
    if (employee) {
      form.setFieldsValue({
        login: employee.login,
        displayName: employee.displayName,
        role: employee.role,
      });
    }
  }, [employee, form]);
  return (
    <Modal
      confirmLoading={confirmLoading}
      destroyOnHidden
      okText={employee ? "Сохранить" : "Создать аккаунт"}
      onCancel={onCancel}
      onOk={() => {
        form.submit();
      }}
      open={open}
      title={employee ? "Изменить сотрудника" : "Новый сотрудник"}
    >
      <EmployeeForm employee={employee} form={form} onFinish={onFinish} />
    </Modal>
  );
}

function EmployeeForm({
  employee,
  form,
  onFinish,
}: {
  employee?: Employee;
  form: ReturnType<typeof Form.useForm<EmployeeFormValues>>[0];
  onFinish: (values: EmployeeFormValues) => void;
}) {
  return (
    <Form<EmployeeFormValues>
      form={form}
      initialValues={{ role: "employee" }}
      layout="vertical"
      onFinish={onFinish}
      preserve={false}
      requiredMark={false}
    >
      <Form.Item label="Имя входа" name="login" rules={[{ required: true, min: 3, max: 64 }]}>
        <Input autoComplete="off" disabled={Boolean(employee)} />
      </Form.Item>
      <Form.Item label="Отображаемое имя" name="displayName" rules={[{ required: true, max: 120 }]}>
        <Input autoComplete="off" />
      </Form.Item>
      <Form.Item label="Роль" name="role" rules={[{ required: true }]}>
        <Select
          options={[
            { label: "Сотрудник", value: "employee" },
            { label: "Аналитик-администратор", value: "analytics_admin" },
          ]}
        />
      </Form.Item>
    </Form>
  );
}

function CredentialModal({
  credential,
  onClose,
}: {
  credential?: TemporaryCredential;
  onClose: () => void;
}) {
  if (!credential) return null;
  const copy = () => {
    void navigator.clipboard.writeText(credential.temporaryPassword);
  };
  return (
    <Modal footer={<Button onClick={onClose}>Готово</Button>} onCancel={onClose} open>
      <div className="credential-content">
        <Text className="eyebrow">Одноразовый показ</Text>
        <Title level={2}>Временный пароль</Title>
        <Alert
          description="После закрытия его нельзя будет прочитать снова. При потере выпустите новый."
          showIcon
          title="Пароль будет показан только сейчас"
          type="warning"
        />
        <Space.Compact block>
          <Input aria-label="Временный пароль" readOnly value={credential.temporaryPassword} />
          <Button aria-label="Копировать временный пароль" icon={<CopyOutlined />} onClick={copy} />
        </Space.Compact>
        <Text type="secondary">Действует 24 часа</Text>
      </div>
    </Modal>
  );
}

function employeeColumns(
  edit: (employee: Employee) => void,
  runAction: (employee: Employee, action: EmployeeAction) => void,
) {
  return [
    {
      title: "Сотрудник",
      key: "employee",
      render: (_: unknown, employee: Employee) => (
        <div>
          <Text strong>{employee.displayName}</Text>
          <Text className="table-subtitle">{employee.login}</Text>
        </div>
      ),
    },
    {
      title: "Роль",
      dataIndex: "role",
      render: (role: Role) => (role === "analytics_admin" ? "Аналитик-администратор" : "Сотрудник"),
    },
    {
      title: "Состояние",
      dataIndex: "state",
      render: (state: Employee["state"]) => <AccountStateTag state={state} />,
    },
    {
      title: "Действия",
      key: "actions",
      render: (_: unknown, employee: Employee) => (
        <EmployeeActions employee={employee} edit={edit} runAction={runAction} />
      ),
    },
  ];
}

function EmployeeActions({
  edit,
  employee,
  runAction,
}: {
  edit: (employee: Employee) => void;
  employee: Employee;
  runAction: (employee: Employee, action: EmployeeAction) => void;
}) {
  const act = (action: EmployeeAction) => {
    runAction(employee, action);
  };
  return (
    <Space wrap size={4}>
      <Button
        onClick={() => {
          edit(employee);
        }}
        size="small"
      >
        Изменить
      </Button>
      <AccessStateButton employee={employee} run={act} />
      {employee.state !== "deactivated" ? (
        <Button
          onClick={() => {
            act("reset");
          }}
          size="small"
        >
          Новый пароль
        </Button>
      ) : null}
      <Button
        onClick={() => {
          act("revoke");
        }}
        size="small"
      >
        Завершить входы
      </Button>
    </Space>
  );
}

function AccessStateButton({
  employee,
  run,
}: {
  employee: Employee;
  run: (action: EmployeeAction) => void;
}) {
  if (employee.state === "deactivated") {
    return (
      <Button
        onClick={() => {
          run("restore");
        }}
        size="small"
      >
        Восстановить
      </Button>
    );
  }
  return (
    <Button
      danger
      onClick={() => {
        run("deactivate");
      }}
      size="small"
    >
      Деактивировать
    </Button>
  );
}
