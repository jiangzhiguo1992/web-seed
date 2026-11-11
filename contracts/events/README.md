# 事件契约（预留）

本目录暂不实现，只固定未来形状，防止派生项目各自发明。

引入异步任务或消息时，事件定义放这里，至少包含：

```yaml
name: note.created
version: 1
producer: adminapi
consumers: [worker]
payload:
  type: object
  additionalProperties: false
  required: [id, title]
  properties:
    id: { type: string, format: uuid }
    title: { type: string }
```

规则：

- 事件名 `<domain>.<past_tense>`
- 必须有 `version`，破坏性变更升版本而非原地改
- payload 默认 `additionalProperties: false`
- 投递语义（至少一次/至多一次）必须写明
- 消费者必须幂等
