import { Tag } from "antd";

import { type Employee } from "./api/client";

export function AccountStateTag({ state }: { state: Employee["state"] }) {
  const labels = {
    pending_activation: "Ожидает активации",
    active: "Активна",
    deactivated: "Деактивирована",
  };
  const colors = { pending_activation: "warning", active: "success", deactivated: "default" };
  return <Tag color={colors[state]}>{labels[state]}</Tag>;
}
