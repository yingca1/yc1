---
name: ascii-callchain
description: >
  生成 ASCII 树形调用链流程图，用于可视化代码执行流程和跨服务交互。
  当用户提到"调用链"、"流程图"、"call chain"、"call trace"、"execution flow"、
  "ASCII diagram"、"画个图"、"追踪调用"、"trace the code path"、"show me the flow"
  时触发。也适用于用户想了解某个功能端到端经过了哪些步骤，或者想为文档生成调用流程图的场景。
---

# ASCII 调用链流程图

将代码调用链可视化为 ASCII 树形图，支持多服务列布局和跨服务箭头。

## 输入模式

支持两种方式获取调用链信息，根据用户意图自动选择：

**模式 A — 自动追踪**：用户给出入口函数或端点，你从代码中逐层读取并追踪调用链。
适用于用户说"帮我追踪 X 的调用链"、"从 Y 函数开始画流程图"等场景。

**模式 B — 描述转图**：用户用文字描述调用流程，你将其格式化为标准 ASCII 图。
适用于用户已经知道流程、只需要排版输出的场景。

两种模式可以混合使用——用户描述大致流程，你读代码补全细节。

## 追踪策略（模式 A）

1. 定位入口函数，读取其实现
2. 识别每个有业务语义的子调用（方法调用、RPC、HTTP 请求、消息发送）
3. 递归展开子调用，直到达到指定深度或叶子函数
4. 遇到跨服务调用（RPC、HTTP、gRPC、消息队列）时，跳转到目标服务继续追踪

**过滤规则**：跳过纯工具性调用（日志、类型转换、参数校验、装饰器包装），只保留能体现业务流程的步骤。判断标准是：如果去掉这个步骤，流程图的读者是否会遗漏关键信息？

**深度控制**：默认展开到所有业务步骤。如果层级过深（>5层），对非关键路径做摘要折叠，用 `...` 或 `(N steps)` 标注。

## 输出格式

### 基本规则

- 每个服务/模块占一列，列顶标注名称 + `---` 分隔线
- 用 `├` `└` `│` 画树形层级，子步骤用 2 空格缩进
- 跨服务调用用 `─────>` 连接到目标列，箭头右侧展开目标服务子流程
- 输出纯 ASCII 等宽文本，禁止 mermaid、plantuml、markdown table

### 注解语法

| 注解 | 含义 | 示例 |
|------|------|------|
| `(xN)` | 循环执行 N 次 | `run_inference (xN)` |
| `(if xxx)` | 条件执行 | `retry (if failed)` |
| `(optional)` | 可选步骤 | `send_notification (optional)` |
| `(async)` | 异步执行 | `emit_event (async)` |
| `(parallel)` | 并行执行 | `fetch_data (parallel)` |

### 单服务示例

```
my-service
---
handle_request
├ authenticate
├ validate_input
├ process
│  ├ load_data
│  ├ transform (xN)
│  └ save_result
└ send_response
```

### 跨服务示例

```
service-a                             service-b
---                                   ---
entry_point
├ step_1
├ step_2
│  └ sub_step
├ remote_call ─────────────────> remote_entry
│                                ├ step_x
│                                │  └ sub_step_x
│                                ├ step_y (xN)
│                                └ step_z
├ step_3 (optional)
└ cleanup
```

### 三服务示例

当涉及三个或更多服务时，按调用顺序从左到右排列：

```
gateway                    backend                    worker
---                        ---                        ---
handle_ws
├ auth
├ route_message ────────> process_message
│                         ├ parse
│                         ├ dispatch ──────────────> execute_task
│                         │                          ├ setup
│                         │                          ├ run (xN)
│                         │                          └ report_result
│                         └ aggregate
└ send_response
```

## 排版技巧

- 列间距保持足够宽（至少 4 个空格），让箭头不显拥挤
- 箭头长度根据列间距自适应
- 同一层级的步骤对齐
- 如果某列的子流程很长，其他列的后续步骤在子流程结束后继续
- 步骤命名用 snake_case，简洁且能传达业务含义

## 执行步骤

1. **确定范围**：明确入口点和涉及的服务/模块
2. **收集调用链**：读代码追踪或从用户描述中提取
3. **筛选步骤**：过滤工具函数，保留业务语义步骤
4. **规划布局**：确定列数、列宽、箭头位置
5. **输出图形**：用代码块包裹，确保等宽对齐
6. **补充说明**：如果有需要解释的步骤，在图下方简要说明
