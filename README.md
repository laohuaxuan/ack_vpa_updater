# ACK VPA Updater

`ack_vpa_updater` 用于读取 ACK 资源画像（Recommendation）建议值，按策略批量更新 Kubernetes 中 `Deployment` / `StatefulSet` 的容器 `requests` 与 `limits`。

## 功能特性

- 基于 ACK `recommendations` CRD 获取 CPU/内存推荐值
- 支持按命名空间、工作负载正则规则过滤目标资源
- 按批次更新，支持成功率阈值控制和 Pod 就绪等待
- 支持配置热更新（运行中修改配置自动生效）
- 支持飞书通知
- 支持结果持久化到本地文件或 MySQL
- 支持两种集群连接方式：
  - `kubeconfig`（本地/外部运行）
  - `in_cluster`（Pod 内 ServiceAccount）

## 工作流程

1. 启动并加载 `config/config.yaml`
2. 连接 Kubernetes（`in_cluster` 优先于 `kubeconfig`）
3. 检查 ACK 资源画像是否启用（`recommendationprofiles`）
4. 按 `check_interval` 周期执行更新任务
5. 根据 `filters` 过滤命名空间和工作负载
6. 读取 recommendation，计算建议资源并更新 Deployment/StatefulSet
7. 输出统计结果，可选发送飞书通知与持久化存储

## 项目结构

```text
.
├── config/
│   └── config.yaml
├── deploy/
│   ├── Dockerfile
│   ├── values/dev.yaml
│   └── yamls/
│       ├── configmap.yaml
│       └── deployment.yaml
├── pkg/
│   ├── ack/            # recommendation 解析与推荐值计算
│   ├── config/         # 配置加载与热更新
│   ├── filter/         # 命名空间/工作负载过滤
│   ├── kubernetes/     # dynamic client 与 K8s 资源操作
│   ├── notification/   # 飞书通知
│   ├── persistence/    # 文件/MySQL持久化
│   └── update/         # 批量更新逻辑
└── main.go
```

## 环境要求

- Go `1.25.6+`
- Kubernetes 集群可访问
- 集群已启用 ACK 资源画像，存在以下 CRD 资源：
  - `autoscaling.alibabacloud.com/v1alpha1` `recommendationprofiles`
  - `autoscaling.alibabacloud.com/v1alpha1` `recommendations`

## 快速开始

### 1) 配置文件

编辑 `config/config.yaml`（示例见下文）。

### 2) 本地运行

```bash
go mod tidy
go run main.go
```

### 3) 容器构建

```bash
docker build -t ack-vpa-updater:latest -f deploy/Dockerfile .
```

## 配置说明

主配置文件：`config/config.yaml`

### 完整示例

```yaml
kubeconfig: "/Users/you/.kube/config"
cluster: "public-dev"
in_cluster: false
api_server: ""

filters:
  namespaces:
    - "zadig"
  exclude_namespaces:
    - "kube-system"
    - "default"
  deployments:
    - ".*"
  exclude_deployments:
    - "others-dev/chat2issue.*"

update_policy:
  batch_size: 5
  success_rate_threshold: 0.5
  pod_ready_timeout: 300
  check_interval: 60
  safetyRedundancy: 0.3

feishu:
  enabled: false
  webhook_url: "https://open.feishu.cn/open-apis/bot/v2/hook/xxx"
  secret: ""

persistence:
  enabled: false
  type: "file" # file 或 mysql
  data_dir: "./data"
  update_log_file: "update_log.json"
  report_file: "report.json"
  mysql:
    host: "localhost"
    port: 3306
    user: "root"
    password: "your-password"
    database: "ack_vpa_updater"
    charset: "utf8mb4"
```

### 关键字段

- `in_cluster`：`true` 时使用 Pod 的 ServiceAccount 连接集群
- `filters.namespaces`：命名空间包含规则（正则）
- `filters.exclude_namespaces`：命名空间排除规则（优先级高于 include）
- `filters.deployments`：工作负载包含规则，格式 `namespace/name`（正则）
- `filters.exclude_deployments`：工作负载排除规则（优先级高于 include）
- `update_policy.batch_size`：每批更新数量
- `update_policy.success_rate_threshold`：批次继续执行阈值（0~1）
- `update_policy.check_interval`：任务轮询间隔（秒）
- `update_policy.pod_ready_timeout`：等待 Pod 就绪超时（秒）
- `update_policy.safetyRedundancy`：推荐值安全冗余系数（0~1）

## filters 规则说明（重点）

过滤器规则是正则匹配，内部实际匹配方式为：

- 命名空间：匹配 `namespace`
- 工作负载：匹配 `namespace/deploymentName`

并且排除规则优先级高于包含规则。

### 命名空间规则组合

- `namespaces` 为空，`exclude_namespaces` 为空：扫描全部命名空间
- `namespaces` 为空，`exclude_namespaces` 非空：扫描“排除后”的命名空间
- `namespaces` 非空，`exclude_namespaces` 为空：仅扫描 include 命中的命名空间
- `namespaces` 非空，`exclude_namespaces` 非空：先排除，再按 include 匹配

### 工作负载规则组合

工作负载规则建议写全路径：`namespace/资源名`。

- `deployments` 为空，`exclude_deployments` 为空：命中当前命名空间下全部工作负载
- `deployments` 非空，`exclude_deployments` 为空：仅更新 include 命中的工作负载
- `exclude_deployments` 非空：先排除，再应用 include 规则

示例：

- 仅更新 `devops` 命名空间全部：`deployments: ["devops/.*"]`
- 排除某个工作负载：`exclude_deployments: ["devops/webqa-agent-placeholder"]`

## Kubernetes 部署说明

仓库提供了基础清单：

- `deploy/yamls/configmap.yaml`
- `deploy/yamls/deployment.yaml`

推荐你补充/确认对应 RBAC（至少需要读取与更新相关资源权限）：

- 读取：`namespaces`、`pods`、`recommendations`、`recommendationprofiles`
- 更新：`deployments`、`statefulsets`

## 常见问题

- 启动报错“未开启资源画像，无法进行分析”
  - 说明未读取到 `recommendationprofiles`，请先确认 ACK 资源画像是否启用
- 配置修改后未生效
  - 程序会监控配置文件变更，确保你修改的是正在挂载/读取的那个 `config.yaml`
- 过滤后没有任何更新对象
  - 优先检查 `exclude_*` 是否把 include 规则都覆盖掉
  - 检查 `deployments` 是否写成了 `namespace/name` 格式
- 单位解析失败
  - 检查 recommendation 或现有资源中 CPU/内存单位是否合法

## 开发建议

- 先在测试命名空间验证 filters 与批次策略
- 再逐步扩大 include 规则范围，避免一次性大规模变更
- 数据持久化目前只实现了CRUD，具体逻辑还未实现