# Stream Fuzz Tests

并发模拟前端通过 OpenAI / Anthropic 官方 Python SDK 发起流式请求，同时经由内部控制接口推送随机文本增量和最终 tool call / assistant 文本，重点检测：

- 流式文本在结束时是否重复尾部片段
- 文本流与最终 `complete` 内容是否一致
- tool call 名称、参数和结束原因是否稳定

示例：

```bash
cd tests
uv run python main.py --requests 40 --concurrency 10 --verbose
```

如果后端启用了 API key，也可以直接传：

```bash
cd tests
uv run tests/main.py --requests 50 --concurrency 10 --tool-call-rate 0.5
```
