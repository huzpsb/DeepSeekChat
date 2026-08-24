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

    fetchDataUrl: function (url) {
        return fetch(url)
            .then(function (r) {
                return r.ok ? r.blob() : null;
            })
            .then(function (blob) {
                if (!blob) return null;
                return new Promise(function (resolve) {
                    var fr = new FileReader();
                    fr.onload = function () {
                        resolve(fr.result);
                    };
                    fr.onerror = function () {
                        resolve(null);
                    };
                    fr.readAsDataURL(blob);
                });
            })
            .catch(function () {
                return null;
            });
    },

    // Copy the app's real stylesheets so the export looks exactly like the
    // app, including the active theme. The theme's relative background
    // image reference (url("bg.jpg")) is replaced by an embedded data URL.
    fetchAppCss: async function () {
        var css = '';
        try {
            var resp = await fetch('/web/css/style.css');
            if (resp.ok) css += await resp.text();
        } catch (e) {
        }
        var name = window.DsTheme ? DsTheme.get() : 'default';
        if (name !== 'default') {
            var base = '/web/themes/' + encodeURIComponent(name) + '/';
            try {
                var themeResp = await fetch(base + 'theme.css');
                if (themeResp.ok) {
                    var themeCss = await themeResp.text();
                    var bg = await this.fetchDataUrl(base + 'bg.jpg');
                    if (bg) {
                        themeCss = themeCss.replace(
                            /url\((['"]?)bg\.jpg\1\)/g,
                            'url("' + bg + '")'
                        );
                    }
                    css += '\n' + themeCss;
                }
            } catch (e) {
            }
        }
        return css;
    },

    exportToHtml: async function () {
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
        // Branding: read the version table from the app itself (single
        // source of truth is the version overlay in index.html).
        var meta = {};
        document.querySelectorAll('.version-table tr').forEach(function (tr) {
            var tds = tr.querySelectorAll('td');
            if (tds.length === 2) meta[tds[0].textContent.trim()] = tds[1].textContent.trim();
        });
        var appName = meta.App || 'DsChat';
        var branding = appName
            + (meta.Version ? ' v' + meta.Version : '')
            + (meta.Author ? ' by ' + meta.Author : '');
        // Copy the app's REAL stylesheets into the export: the exported DOM
        // is a clone of the live one, so all classes match and the result
        // always looks exactly like what the user sees, theme included.
        var appCss = await this.fetchAppCss();
        var html = '<!DOCTYPE html>\n'
            + '<html lang="en">\n<head>\n<meta charset="UTF-8">\n<meta name="viewport" content="width=device-width, initial-scale=1.0">\n'
            + '<title>' + this.escHtml(appName) + ' - ' + t + '</title>\n'
            + '<style>\n'
            + 'body{padding:0}\n'
            + '#export-header{position:fixed;top:0;left:0;right:0;background:var(--bg-secondary);border-bottom:1px solid var(--border);padding:16px 24px;z-index:10}\n'
            + '#export-header h1{font-size:18px;font-weight:600;color:var(--accent)}\n'
            + '#export-header .export-meta{font-size:11px;color:var(--text-secondary);margin-top:4px}\n'
            + '#export-messages{padding:96px 24px 40px;min-height:100vh;display:flex;flex-direction:column;gap:8px}\n'
            + appCss
            /* export page scrolls (unlike the app shell), so the 100%
               height constraint must go or the fixed background misbehaves */
            + 'html,body{height:auto;min-height:100%}\n'
            /* full-width glass; messages capped only enough for the
               left/right alignment to read */
            + '#export-messages .message{max-width:min(85%,1200px)}\n'
            + '.msg-actions{display:none}\n'
            + '</style>\n</head>\n<body>\n'
            + '<div id="export-header"><h1>' + this.escHtml(appName) + ' - ' + t + '</h1><div class="export-meta">Exported on ' + new Date().toISOString().split('T')[0] + ' | ' + count + ' messages | ' + this.escHtml(branding) + '</div></div>\n'
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
