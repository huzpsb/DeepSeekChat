DsChat
---

[English Version](README.MD)

DsChat是一个高度自由、支持MCP功能的DeepSeek-v4-pro的Agentic用户界面。

## 画廊

像其他任何一个Agentic客户端一样工作。连接到VsCode/GoLand/PyCharm等IDE，像ClaudeCode/OpenCode一样工作.
![](/doc/agent.png)

比其他任何一个Agentic客户端更自由。随时在任何位置插入任何角色的消息，修改任何一个回应。
![](/doc/edit.png)

## 理念

- 保持控制。DsChat是唯一一个允许你在gui中完整地控制每一条消息的MCP/Agentic客户端。你可以随时修改任何一条消息，AI的回应与工具调用并不例外。这使得你可以完全控制对话的走向和内容。
- 完全透明。DsChat的每一条消息是否发送至服务器由且只由send_to_server字段控制。没有隐藏的上下文压缩与消息检索机制。你可以理解每一次AI回应的原因。
-

简单易用。我们理解上述功能虽然强大，但是也会带来不小的心智负担。因此，DsChat默认工作在Read模式下，功能由Write和Sudo模式逐步开放。你可以逐步学习这两个模式的功能以控制对话，或者留着Read模式下享受你熟悉的Agentic体验。
-
安全可靠。DsChat没有供应链，因而是唯一一个原则上不可能供应链攻击的MCP/Agentic客户端。0依赖的Go程序，0外部依赖的Html/JavaScript。你可以完全信任DsChat的安全性和可靠性。工具可以设置为需要求用户确认后才能调用，或者直接调用。你完全控制工具的调用。

## 快速开始

1. 下载DsChat的最新版本：[Releases](https://github.com/huzpsb/DeepSeekChat/releases)。
2. 复制`config_sample.json`为`config.json`，并根据需要修改配置文件。
3. 浏览器打开127.0.0.1，选择左上角的Tools按钮，按需启用工具并点击Reload。
4. 开始您的Agentic体验。

## 未来计划

- 工具链：提供一系列与此项目分离的Tools。
- 前端：支持浅色模式和主题自定义功能。
- 后端：支持Skills和其他非SSE格式的远程Tools。

## 贡献

欢迎任何形式的贡献！请不要觉得提出Issue是在为我们带来不便，我们非常欢迎任何形式的反馈和建议！  
当然，如果您有兴趣直接参与开发，也欢迎随时提交Pull Request。