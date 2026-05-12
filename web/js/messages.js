// messages.js - message rendering with marked.js
var Messages = {
    render: function (messages, container) {
        container.innerHTML = '';
        if (messages == null) {
            var emptyDiv = document.createElement('div');
            emptyDiv.className = 'empty-state';
            emptyDiv.style.cssText = 'color:var(--text-secondary);text-align:center;padding:40px';
            emptyDiv.textContent = '请新建或打开会话';
            container.appendChild(emptyDiv);
            return;
        }
        if (messages.length === 0) {
            var isReadonly = window.DsApp && window.DsApp.getMode && window.DsApp.getMode() === 'readonly';
            var emptyDiv = document.createElement('div');
            emptyDiv.className = 'empty-state';
            emptyDiv.style.cssText = 'color:var(--text-secondary);text-align:center;padding:40px';
            if (isReadonly) {
                emptyDiv.textContent = '请开始聊天';
            } else {
                emptyDiv.innerHTML = '<button class="btn-insert-empty" style="background:var(--bg-primary);color:var(--accent);border:1px solid var(--accent);padding:6px 16px;border-radius:4px;cursor:pointer;font-size:13px">Insert First Message</button>';
            }
            container.appendChild(emptyDiv);
            return;
        }
        messages.forEach((msg, idx) => {
            try {
                const isLast = idx === messages.length - 1;
                const el = this.renderMessage(msg, idx, isLast);
                container.appendChild(el);
            } catch (e) {
                console.error('Error rendering message', idx, e);
            }
        });
        container.scrollTop = container.scrollHeight;
    },

    renderMessage: function (msg, idx, isLast) {
        const div = document.createElement('div');
        div.className = 'message role-' + msg.role;
        div.dataset.index = idx;
        div.dataset.role = msg.role;
        div.dataset.rawMessage = JSON.stringify(msg);

        const header = document.createElement('div');
        header.className = 'msg-header';
        header.innerHTML = '<span class="msg-role">' + msg.role.toUpperCase() + '</span>';

        const tags = document.createElement('span');
        tags.className = 'msg-tags';

        if (msg.send_to_server === false) {
            const tag = document.createElement('span');
            tag.className = 'msg-tag no-server';
            tag.textContent = 'no-server';
            tags.appendChild(tag);
        }

        if (msg.role === 'assistant' && msg.approved) {
            const tag = document.createElement('span');
            tag.className = 'msg-tag approved';
            tag.textContent = 'approved';
            tags.appendChild(tag);
        }

        if (msg.role === 'tool' && msg.name) {
            const tag = document.createElement('span');
            tag.className = 'msg-tag';
            tag.textContent = msg.name;
            tags.appendChild(tag);
        }

        header.appendChild(tags);
        div.appendChild(header);

        if (msg.reasoning_content) {
            const reasoning = document.createElement('div');
            reasoning.className = 'reasoning-block';
            const toggle = document.createElement('div');
            toggle.className = 'reasoning-toggle';
            toggle.textContent = 'Reasoning \u25B6';
            const content = document.createElement('div');
            content.style.display = 'none';
            content.innerHTML = marked.parse(msg.reasoning_content);
            content.dataset.rawText = msg.reasoning_content;
            toggle.addEventListener('click', function () {
                if (content.style.display === 'none') {
                    content.style.display = 'block';
                    toggle.textContent = 'Reasoning \u25BC';
                } else {
                    content.style.display = 'none';
                    toggle.textContent = 'Reasoning \u25B6';
                }
            });
            reasoning.appendChild(toggle);
            reasoning.appendChild(content);
            div.appendChild(reasoning);
        }

        const content = document.createElement('div');
        content.className = 'msg-content';
        if (msg.content) {
            if (msg.role === 'tool') {
                content.innerHTML = '<pre><code>' + this.escHtml(msg.content) + '</code></pre>';
                content.style.display = 'none';
                var toolToggle = document.createElement('span');
                toolToggle.className = 'tool-result-toggle';
                toolToggle.textContent = ' \u25B6';
                toolToggle.addEventListener('click', function (e) {
                    e.stopPropagation();
                    if (content.style.display === 'none') {
                        content.style.display = 'block';
                        toolToggle.textContent = ' \u25BC';
                    } else {
                        content.style.display = 'none';
                        toolToggle.textContent = ' \u25B6';
                    }
                });
                header.appendChild(toolToggle);
            } else {
                content.innerHTML = marked.parse(msg.content);
            }
            content.dataset.rawText = msg.content;
        }
        div.appendChild(content);

        if (msg.tool_calls && msg.tool_calls.length > 0) {
            const tcBlock = document.createElement('div');
            tcBlock.className = 'tool-calls-block';
            const tcToggle = document.createElement('div');
            tcToggle.className = 'tool-calls-toggle';
            tcToggle.textContent = 'Tool Calls (' + msg.tool_calls.length + ') \u25B6';
            const tcl = document.createElement('div');
            tcl.className = 'tool-calls-list';
            tcl.style.display = 'none';
            msg.tool_calls.forEach(tc => {
                const item = document.createElement('div');
                item.className = 'tool-call-item';
                item.dataset.toolCallId = tc.id;
                item.innerHTML = '<div class="tool-call-name">' + this.escHtml(tc.function.name) + '</div>'
                    + '<div class="tool-call-args">' + this.escHtml(this.formatArgs(tc.function.arguments)) + '</div>';
                tcl.appendChild(item);
            });
            tcToggle.addEventListener('click', function () {
                if (tcl.style.display === 'none') {
                    tcl.style.display = 'flex';
                    tcToggle.textContent = 'Tool Calls (' + msg.tool_calls.length + ') \u25BC';
                } else {
                    tcl.style.display = 'none';
                    tcToggle.textContent = 'Tool Calls (' + msg.tool_calls.length + ') \u25B6';
                }
            });
            tcBlock.appendChild(tcToggle);
            tcBlock.appendChild(tcl);
            div.appendChild(tcBlock);
        }

        const actions = document.createElement('div');
        actions.className = 'msg-actions';
        var isReadonly = window.DsApp && window.DsApp.getMode && window.DsApp.getMode() === 'readonly';
        var showDel = !isReadonly || isLast;
        if (isReadonly) {
            if (msg.role === 'assistant') {
                actions.innerHTML = ''
                    + '<button class="btn-approve-msg" title="Toggle Approve">' + (msg.approved ? 'Unapprove' : 'Approve') + '</button>'
                    + (showDel ? '<button class="btn-del-msg" title="Delete">Del</button>' : '')
                    + '<button class="btn-copy-msg" title="Copy content">Copy</button>';
            } else {
                actions.innerHTML = ''
                    + (showDel ? '<button class="btn-del-msg" title="Delete">Del</button>' : '')
                    + '<button class="btn-copy-msg" title="Copy content">Copy</button>';
            }
        } else {
            actions.innerHTML = '<button class="btn-edit-msg" title="Edit">Edit</button>'
                + '<button class="btn-del-msg" title="Delete">Del</button>'
                + '<button class="btn-insb-msg" title="Insert Before">InsB</button>'
                + '<button class="btn-insa-msg" title="Insert After">InsA</button>';
        }
        div.appendChild(actions);

        return div;
    },

    formatArgs: function (args) {
        try {
            return JSON.stringify(JSON.parse(args), null, 2);
        } catch (e) {
            return args;
        }
    },

    escHtml: function (s) {
        const d = document.createElement('div');
        d.textContent = s;
        return d.innerHTML;
    },

    highlightErrors: function (errors) {
        document.querySelectorAll('.highlight-error').forEach(el => el.classList.remove('highlight-error'));
        errors.forEach(err => {
            if (err.message_index >= 0) {
                const el = document.querySelector('.message[data-index="' + err.message_index + '"]');
                if (el) {
                    el.classList.add('highlight-error');
                }
            }
            if (err.tool_call_id) {
                const tcEl = document.querySelector('.tool-call-item[data-tool-call-id="' + err.tool_call_id + '"]');
                if (tcEl) tcEl.classList.add('highlight-error');
            }
        });
        setTimeout(() => {
            document.querySelectorAll('.highlight-error').forEach(el => el.classList.remove('highlight-error'));
        }, 5000);
    }
};
