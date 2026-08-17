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
                if (window.NoobMode && window.NoobMode.isActive()) {
                    if (msg.role !== 'user' && msg.role !== 'assistant' && msg.role !== 'error') return;
                    if (msg.role === 'assistant' && !msg.content) return;
                }
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

        var isNoob = window.NoobMode && window.NoobMode.isActive();

        if (msg.reasoning_content && !isNoob) {
            const reasoning = document.createElement('div');
            reasoning.className = 'reasoning-block';
            const toggle = document.createElement('div');
            toggle.className = 'reasoning-toggle';
            toggle.textContent = 'Reasoning \u25B6';
            const content = document.createElement('div');
            content.className = 'msg-content'; // reuse markdown typography
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

        if (msg.tool_calls && msg.tool_calls.length > 0 && !isNoob) {
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
        if (isNoob) showDel = false;
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
        // Snapshot the active theme: resolve the CSS variables now, so the
        // exported standalone file looks exactly like what the user
        // currently sees, whatever theme is selected.
        var computed = getComputedStyle(document.documentElement);
        var themeVars = [
            '--bg-primary', '--bg-secondary', '--bg-tertiary',
            '--text-primary', '--text-secondary', '--accent', '--accent-dim',
            '--success', '--warning', '--border',
            '--msg-system', '--msg-user', '--msg-user-text', '--msg-assistant', '--msg-tool',
            '--text-on-success', '--text-on-warning'
        ];
        var rootCss = ':root{';
        themeVars.forEach(function (v) {
            rootCss += v + ':' + computed.getPropertyValue(v).trim() + ';';
        });
        rootCss += '}\n';
        var html = '<!DOCTYPE html>\n'
            + '<html lang="en">\n<head>\n<meta charset="UTF-8">\n<meta name="viewport" content="width=device-width, initial-scale=1.0">\n'
            + '<title>DsChat - ' + t + '</title>\n'
            + '<style>\n'
            + rootCss
            + '*,*::before,*::after{box-sizing:border-box;margin:0;padding:0}\n'
            + 'body{font-family:"Segoe UI",system-ui,-apple-system,sans-serif;background:var(--bg-primary);color:var(--text-primary);font-size:14px;line-height:1.5;padding:80px 0 40px 0}\n'
            + '#export-header{position:fixed;top:0;left:0;right:0;background:var(--bg-secondary);border-bottom:1px solid var(--border);padding:16px 24px;z-index:10}\n'
            + '#export-header h1{font-size:18px;font-weight:600;color:var(--accent)}\n'
            + '#export-header .export-meta{font-size:11px;color:var(--text-secondary);margin-top:4px}\n'
            + '#export-messages{max-width:900px;margin:0 auto;padding:0 16px;display:flex;flex-direction:column;gap:8px}\n'
            + '.message{padding:12px 16px;border-radius:8px;max-width:85%;position:relative}\n'
            + '.message.role-system{background:var(--msg-system);align-self:center;max-width:95%;text-align:center;font-style:italic}\n'
            + '.message.role-user{background:var(--msg-user);color:var(--msg-user-text);align-self:flex-end}\n'
            + '.message.role-assistant{background:var(--msg-assistant);align-self:flex-start}\n'
            + '.message.role-tool{background:var(--msg-tool);align-self:flex-start;font-family:Consolas,"Fira Code",monospace;font-size:13px;white-space:pre-wrap}\n'
            + '.msg-header{display:flex;align-items:center;gap:8px;margin-bottom:4px;font-size:11px;color:var(--text-secondary)}\n'
            + '.msg-role{font-weight:600;text-transform:uppercase}\n'
            + '.msg-tags{display:flex;gap:4px}\n'
            + '.msg-tag{padding:1px 6px;border-radius:3px;font-size:10px;background:var(--bg-tertiary);color:var(--text-secondary)}\n'
            + '.msg-tag.no-server{background:var(--warning);color:var(--text-on-warning)}\n'
            + '.msg-tag.approved{background:var(--success);color:var(--text-on-success)}\n'
            + '.msg-content{word-break:break-word}\n'
            + '.msg-content>:first-child{margin-top:0}\n'
            + '.msg-content>:last-child{margin-bottom:0}\n'
            + '.msg-content p{margin:6px 0}\n'
            + '.msg-content h1,.msg-content h2,.msg-content h3,.msg-content h4{margin:14px 0 6px;line-height:1.3;font-weight:600}\n'
            + '.msg-content h1{font-size:19px}\n'
            + '.msg-content h2{font-size:17px}\n'
            + '.msg-content h3{font-size:15px}\n'
            + '.msg-content h4{font-size:14px}\n'
            + '.msg-content ul,.msg-content ol{margin:6px 0;padding-left:22px}\n'
            + '.msg-content li{margin:3px 0}\n'
            + '.msg-content li>p{margin:0}\n'
            + '.msg-content a{color:var(--accent)}\n'
            + '.msg-content blockquote{margin:8px 0;padding:4px 12px;background:var(--bg-secondary);border-left:3px solid var(--accent-dim);border-radius:0 4px 4px 0;color:var(--text-secondary)}\n'
            + '.msg-content hr{margin:12px 0;border:none;border-top:1px solid var(--border)}\n'
            + '.msg-content table{margin:8px 0;border-collapse:collapse;font-size:13px}\n'
            + '.msg-content th,.msg-content td{border:1px solid var(--border);padding:5px 10px;text-align:left}\n'
            + '.msg-content th{background:var(--bg-secondary);font-weight:600}\n'
            + '.msg-content img{max-width:100%}\n'
            + '.msg-content pre{margin:8px 0;background:var(--bg-secondary);border:1px solid var(--border);border-radius:4px;padding:8px;overflow-x:auto}\n'
            + '.msg-content code{background:var(--bg-secondary);padding:1px 4px;border-radius:3px;font-family:Consolas,"Fira Code",monospace;font-size:13px}\n'
            + '.reasoning-block{margin-bottom:8px;padding:8px;background:var(--bg-secondary);border-left:3px solid var(--accent-dim);border-radius:4px;font-size:12px;color:var(--text-secondary)}\n'
            + '.reasoning-toggle{cursor:default;color:var(--accent);font-size:11px;user-select:none}\n'
            + '.tool-calls-block{margin-top:8px;padding:8px;background:var(--bg-secondary);border-left:3px solid var(--accent-dim);border-radius:4px}\n'
            + '.tool-calls-toggle{cursor:default;color:var(--accent);font-size:11px;user-select:none;margin-bottom:6px}\n'
            + '.tool-calls-list{display:flex;flex-direction:column;gap:4px}\n'
            + '.tool-call-item{padding:6px 8px;background:var(--bg-secondary);border-radius:4px;font-size:12px;border:1px solid var(--border)}\n'
            + '.tool-call-item .tool-call-name{font-weight:600;color:var(--accent)}\n'
            + '.tool-call-item .tool-call-args{color:var(--text-secondary);font-family:Consolas,"Fira Code",monospace;font-size:11px;white-space:pre-wrap}\n'
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
