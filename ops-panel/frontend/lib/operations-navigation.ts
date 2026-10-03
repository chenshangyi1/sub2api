import {
  ChartNoAxesCombined,
  ImagePlus,
  Radar,
  UsersRound,
  Wrench,
  type LucideIcon,
} from "lucide-react"

export interface OperationsNavigationItem {
  label: string
  description: string
  path: string
  icon: LucideIcon
}

export const operationsNavigation: readonly OperationsNavigationItem[] = [
  {
    label: "目标账号",
    description: "账号池、余额告警与调度状态",
    path: "/target-accounts",
    icon: UsersRound,
  },
  {
    label: "增强探测",
    description: "轻量、重量与能力检测",
    path: "/target-probes",
    icon: Radar,
  },
  {
    label: "主服务分析",
    description: "主服务成本、利润与请求质量",
    path: "/target-analytics",
    icon: ChartNoAxesCombined,
  },
  {
    label: "诊断中心",
    description: "服务、Worker 与清理任务",
    path: "/diagnostics",
    icon: Wrench,
  },
  {
    label: "媒体生成",
    description: "用网关密钥调用生图 / 生视频",
    path: "/media",
    icon: ImagePlus,
  },
] as const
