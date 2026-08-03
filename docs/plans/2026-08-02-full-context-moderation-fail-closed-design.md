# 全上下文内容审核与失败关闭设计

> 2026-08-03 附件修订：本文的“完整语义上下文”指完整、有序、可见的文本和受控附件标记，不代表读取图片像素或文件字节。附件具体边界以 `2026-08-03-text-only-moderation-attachment-pass-through-design.md` 为准，并取代旧的 structured-image 一律本地阻止口径。

## 目标

在 Omni 用户版把内容审核变成真正的上游安全边界：对准备发送给下游模型的完整语义上下文进行审核；`pre_block` 范围内的审核服务在最多三次总尝试后仍失败时，本地返回错误并终止，不选择或请求下游账号。

## 第一版边界

- 仅修改 `dev/omni`，不向其他产品分支同步。
- 审核 OpenAI Responses、Chat Completions、Anthropic Messages、Gemini、图片请求和 WebSocket 请求已有入口。
- Prompt 审计表仍只记录本轮用户原文；本设计只改变内容审核输入，不改变 Prompt 审计口径。
- `observe` 保持非阻断，用于上线前测量；`pre_block` 对其现有分组范围执行失败关闭。
- `sample_rate` 只用于 `observe` 抽样；`pre_block` 对范围内每个请求都调用审核，不能因抽样配置绕过。
- 不在第一版引入云端和 Windows 之间的有状态分片、会话重建或自定义 DP 协议。

## 完整审核输入

Sub2API 在现有账号选择和上游发送之前，把请求中的可见语义按原顺序构造成稳定文本：

- system、developer、user、assistant 等消息文本；
- tool/function 调用名称、参数和工具返回；
- Responses `instructions`、函数调用与函数输出；
- Gemini system instruction、functionCall 与 functionResponse；
- OpenAI Images 和 Grok media 的 prompt；图片、文档、文件、上传和远程引用只生成受控附件标记。

每段保留稳定角色标记。旧上下文未变时，新一轮只在末尾追加稳定片段，便于 Windows 侧 vLLM 自动前缀缓存复用计算。不得因最后一项是 `assistant`、`tool`、工具输出或用户只输入 `Continue` 而跳过历史上下文。

OpenAI Chat/Responses/Images、Anthropic、Gemini、Grok 和 tool traffic 中的附件只保留规范化 `kind`、MIME、来源类别及扩展名；不把 URL/URI、file ID 值、文件名、Base64、二进制、headers、cookies 或 credentials 送给 MiniMax。原始请求体不因审核投影而改变：文本严格 `allow` 后，原附件按正常路径继续上游；`block` 或 `review` 则阻止整个请求。附件存在本身不是 allow、block、review 或审核服务错误的依据。

删除现有 12,000 字符静默截断。Sub2API 不再把被截掉的后半段当作不存在；若 Windows 适配器或模型的显式容量限制被触发，该请求进入审核失败路径，在 `pre_block` 中本地终止。

第一版明确接受纯附件攻击的残余风险：若危险指令只存在于图片像素或文件字节而可见文本安全，文本审核可能允许原附件到达上游。第一版不做 OCR、远程抓取、PDF/文档提取、压缩包或可执行文件分析、恶意软件扫描，也不得声称覆盖这些内容。adapter 被直接发送 structured image 时仍返回本地 flagged 结果作为纵深防御，但这不是正常 Sub2API 附件路径，也不是图片审核能力。

## 传输与延迟

Sub2API 对超过 1 KiB 的 Moderations JSON 使用 gzip best-speed 压缩，Windows 适配器同时限制压缩前和解压后的请求体，防止压缩炸弹。小请求保持普通 JSON，避免压缩开销大于收益。

第一版每轮仍发送完整规范化语义文本。vLLM 前缀缓存减少重复推理计算，但不减少云端到 Windows 的字节数；gzip 负责第一版的网络体积优化。只有实测表明请求体传输是主要延迟来源时，第二版才增加带会话 ID、前缀哈希、严格顺序和丢失恢复的增量协议。

“1 秒”是健康暖机请求的目标预算，不是写死的拒绝阈值。网络、队列和模型推理分别记录延迟，Windows 联调后用实际 P95/P99 设置单次超时；第一版不因为总耗时刚超过 1 秒就把一个仍在正常执行的审核判为安全或绕过审核。

## 失败策略

- `retry_count=2` 表示首次调用加两次重试，共最多三次总尝试。
- 网络错误、超时、429 和 5xx 等可重试失败，即使只有一个审核 Key，也允许在本次调用内完成剩余尝试；健康状态冻结只影响后续新请求。
- 400、401 和 403 等确定性配置或认证错误可直接失败，不做无意义重试。
- `pre_block` 中无审核 Key、超时、网络错误、非 2xx、空结果、非法 JSON 或解析错误均返回本地 503，action 记为 `error`，不得继续上游。
- `observe` 继续记录错误并放行，便于正式启用前收集质量和延迟证据。
- 分组仍由现有 `all_groups`、`group_ids` 和 `excluded_group_ids` 决定；失败关闭只作用于已经进入 `pre_block` 审核范围的请求。

## 验收

- 危险历史加最后一句 `Continue`、工具输出结尾和 assistant 结尾都必须把完整历史送审。
- 超过 12,000 字符的尾部内容必须进入送审文本，不得静默丢失。
- 单审核 Key 连续返回可重试错误时必须实际发起三次，随后返回本地 503。
- 审核失败的请求不得发生账号选择、usage log 或 OAI 网络请求。
- 大于 1 KiB 的审核请求必须使用 gzip，Windows 适配器能解压并拒绝损坏或解压超限的数据。
- 安全文本加附件、纯附件请求不得仅因附件存在而失败；危险可见文本加附件必须在账号选择前阻止整个请求。
- MiniMax 请求只能包含文本和受控标记，不得包含附件字节、原始 URL/URI、file ID 值、文件名、Base64、headers、cookies 或 credentials。
- classifier policy revision 必须在 readiness 和每个成功响应中与 Sub2API 期望值匹配；投影 revision 和 classifier revision 同时进入 chunk hash 与 Redis namespace，旧策略缓存不得复用。
- 所有 HTTP、WebSocket、重试和 fallback 路径均需验证无法绕过。
