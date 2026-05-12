## DsChat

[中文版](README_CN.MD)

DsChat is a highly flexible, MCP-compatible Agentic UI for DeepSeek-v4-pro.

## Gallery

**Works like any other Agentic client.** Connect to IDEs like VS Code, GoLand, or PyCharm to work just like ClaudeCode
or OpenCode.
![](/doc/agent.png)

**More freedom than any other Agentic client.** Insert messages for any role at any position or modify any response at
any time.
![](/doc/edit.png)

---

## Philosophy

* **Maintain Control.** DsChat is the only MCP/Agentic client that grants you full control over every message within a
  GUI. You can modify any message at any time—AI responses and tool calls included. This gives you absolute command over
  the direction and content of the conversation.
* **Total Transparency.** Whether a message is sent to the server is controlled solely by the `send_to_server` field.
  There are no hidden context compression or message retrieval mechanisms. You will always understand exactly why the AI
  responded the way it did.
* **Simple & Easy to Use.** We understand that while powerful, these features can increase cognitive load. Therefore,
  DsChat defaults to **Read mode**. Features are gradually unlocked through **Write** and **Sudo** modes. You can learn
  these modes over time to master the conversation or simply stay in Read mode for a familiar Agentic experience.
* **Safe & Reliable.** With no external supply chain, DsChat is in principle the only MCP/Agentic client immune to
  supply chain attacks. It is a zero-dependency Go program with zero external HTML/JavaScript dependencies. You can
  fully trust its security. Tools can be set to require user confirmation or run automatically—you retain total control
  over tool execution.

---

## Quick Start

1. Download the latest version of DsChat: [Releases]().
2. Copy `config_sample.json` to `config.json` and modify the configuration as needed.
3. Open `127.0.0.1` in your browser, select the **Tools** button in the top-left corner, enable tools as needed, and
   click **Reload**.
4. Begin your Agentic experience.

---

## Roadmap

* **Toolchain:** Provide a series of standalone tools separate from the core project.
* **Frontend:** Support Light Mode and theme customization.
* **Backend:** Support "Skills" and other remote tools using non-SSE formats.

---

## Contribution

Contributions of any kind are welcome! Please don't hesitate to open an Issue; we highly value all feedback and
suggestions. If you're interested in direct development, feel free to submit a Pull Request at any time.