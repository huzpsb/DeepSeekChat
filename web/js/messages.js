// messages.js - message rendering with marked.js
marked.setOptions({breaks: true});
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
    },

    exportToHtml: function () {
        var title = window.ChatList ? ChatList.getCurrentTitle() : null;
        if (!title) {
            alert('No chat open to export.');
            return;
        }

        var container = document.getElementById('messages');
        var msgEls = container.querySelectorAll('.message');
        var count = msgEls.length;
        var clones = [].map.call(msgEls, function (el) {
            var clone = el.cloneNode(true);
            clone.querySelectorAll('.msg-content pre code').forEach(function (code) {
                var text = code.textContent || '';
                if (text.length > 3000) {
                    code.innerHTML = Messages.escHtml(text.substring(0, 1000))
                        + '\n\n... [truncated ' + (text.length - 2000).toLocaleString() + ' chars] ...\n\n'
                        + Messages.escHtml(text.substring(text.length - 1000));
                }
            });

            clone.removeAttribute('data-raw-message');
            clone.querySelectorAll('[data-raw-text]').forEach(function (c) {
                c.removeAttribute('data-raw-text');
            });
            clone.querySelectorAll('.msg-actions').forEach(function (c) {
                c.remove();
            });
            clone.classList.remove('highlight-error', 'highlight-success');

            return clone.outerHTML;
        }).join('\n');

        var t = this.escHtml(title);
        var html = '<!DOCTYPE html>\n'
            + '<html lang="en">\n<head>\n<meta charset="UTF-8">\n<meta name="viewport" content="width=device-width, initial-scale=1.0">\n'
            + '<title>DsChat - ' + t + '</title>\n'
            + '<style>\n'
            + '*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}\n'
            + 'body{font-family:"Segoe UI",system-ui,-apple-system,sans-serif;background:#1a1a2e;color:#e0e0e0;font-size:14px;line-height:1.5;padding:80px 0 40px 0}\n'
            + '#export-header{position:fixed;top:0;left:0;right:0;background:#16213e;border-bottom:1px solid #2a2a4a;padding:16px 24px;z-index:10}\n'
            + '#export-header h1{font-size:18px;font-weight:600;color:#4fc3f7}\n'
            + '#export-header .export-meta{font-size:11px;color:#a0a0a0;margin-top:4px}\n'
            + '#export-messages{max-width:900px;margin:0 auto;padding:0 16px;display:flex;flex-direction:column;gap:8px}\n'
            + '.message{padding:12px 16px;border-radius:8px;max-width:85%;position:relative}\n'
            + '.message.role-system{background:#2d1b69;align-self:center;max-width:95%;text-align:center;font-style:italic}\n'
            + '.message.role-user{background:#1a3a5c;align-self:flex-end}\n'
            + '.message.role-assistant{background:#1a2a1a;align-self:flex-start}\n'
            + '.message.role-tool{background:#3a2a0a;align-self:flex-start;font-family:Consolas,"Fira Code",monospace;font-size:13px;white-space:pre-wrap}\n'
            + '.msg-header{display:flex;align-items:center;gap:8px;margin-bottom:4px;font-size:11px;color:#a0a0a0}\n'
            + '.msg-role{font-weight:600;text-transform:uppercase}\n'
            + '.msg-tags{display:flex;gap:4px}\n'
            + '.msg-tag{padding:1px 6px;border-radius:3px;font-size:10px;background:#0f3460;color:#a0a0a0}\n'
            + '.msg-tag.no-server{background:#f39c12;color:#000}\n'
            + '.msg-tag.approved{background:#2ecc71;color:#000}\n'
            + '.msg-content{word-break:break-word}\n'
            + '.msg-content p{margin:4px 0}\n'
            + '.msg-content pre{background:#16213e;border:1px solid #2a2a4a;border-radius:4px;padding:8px;overflow-x:auto}\n'
            + '.msg-content code{background:#16213e;padding:1px 4px;border-radius:3px;font-family:Consolas,"Fira Code",monospace;font-size:13px}\n'
            + '.reasoning-block{margin-bottom:8px;padding:8px;background:#16213e;border-left:3px solid #2a8fc7;border-radius:4px;font-size:12px;color:#a0a0a0}\n'
            + '.reasoning-toggle{cursor:default;color:#4fc3f7;font-size:11px;user-select:none}\n'
            + '.tool-calls-block{margin-top:8px;padding:8px;background:#16213e;border-left:3px solid #2a8fc7;border-radius:4px}\n'
            + '.tool-calls-toggle{cursor:default;color:#4fc3f7;font-size:11px;user-select:none;margin-bottom:6px}\n'
            + '.tool-calls-list{display:flex;flex-direction:column;gap:4px}\n'
            + '.tool-call-item{padding:6px 8px;background:#16213e;border-radius:4px;font-size:12px;border:1px solid #2a2a4a}\n'
            + '.tool-call-item .tool-call-name{font-weight:600;color:#4fc3f7}\n'
            + '.tool-call-item .tool-call-args{color:#a0a0a0;font-family:Consolas,"Fira Code",monospace;font-size:11px;white-space:pre-wrap}\n'
            + '.msg-actions{display:none}\n'
            + '</style>\n</head>\n<body>\n'
            + '<div id="export-header"><h1>HsChat - ' + t + '</h1><div class="export-meta">Exported on ' + new Date().toISOString().split('T')[0] + ' | ' + count + ' messages</div></div>\n'
            + '<div id="export-messages">\n'
            + clones
            + '\n</div>\n'
            + '<script>\n'
            + 'document.querySelectorAll(".reasoning-toggle").forEach(function(t){t.style.cursor="pointer";t.addEventListener("click",function(){var c=this.nextElementSibling;if(c.style.display==="none"){c.style.display="block";this.textContent=this.textContent.replace("\\u25B6","\\u25BC")}else{c.style.display="none";this.textContent=this.textContent.replace("\\u25BC","\\u25B6")}})});\n'
            + 'document.querySelectorAll(".tool-calls-toggle").forEach(function(t){t.style.cursor="pointer";t.addEventListener("click",function(){var c=this.nextElementSibling;if(c.style.display==="none"){c.style.display="flex";this.textContent=this.textContent.replace("\\u25B6","\\u25BC")}else{c.style.display="none";this.textContent=this.textContent.replace("\\u25BC","\\u25B6")}})});\n'
            + 'document.querySelectorAll(".tool-result-toggle").forEach(function(t){t.style.cursor="pointer";t.addEventListener("click",function(e){e.stopPropagation();var c=this.parentElement.parentElement.querySelector(".msg-content");if(c.style.display==="none"){c.style.display="block";this.textContent=" \\u25BC"}else{c.style.display="none";this.textContent=" \\u25B6"}})});\n'
            + '</' + 'script>\n'
            + '</body>\n</html>';

        var blob = new Blob([html], {type: 'text/html;charset=utf-8'});
        var url = URL.createObjectURL(blob);
        var a = document.createElement('a');
        a.href = url;
        a.download = title.replace(/[\\/:*?"<>|]/g, '_') + '.html';
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
    }
};
