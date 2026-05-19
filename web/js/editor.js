// editor.js - edit/delete/insert messages
(function () {
    'use strict';

    var messagesContainer = document.getElementById('messages');
    var editorOverlay = document.getElementById('editor-overlay');
    var editorBody = document.getElementById('editor-body');
    var toolSchemaCache = null;

    var editingMsg = null;
    var editingIndex = -1;
    var toolCallRowCounter = 0;

    // ---- tool schema helpers ----

    async function fetchToolSchemas() {
        if (toolSchemaCache) return toolSchemaCache;
        try {
            var resp = await fetch('/api/mcp/tools');
            var tools = await resp.json();
            toolSchemaCache = {};
            tools.forEach(function (t) {
                if (t.input_schema) {
                    toolSchemaCache[t.tool_name] = t.input_schema;
                }
            });
        } catch (e) {
            toolSchemaCache = {};
        }
        return toolSchemaCache;
    }

    function isSimpleSchema(schema) {
        if (!schema || schema.type !== 'object' || !schema.properties) return false;
        var keys = Object.keys(schema.properties);
        if (keys.length === 0) return true;
        var simpleTypes = ['string', 'number', 'integer', 'boolean'];
        for (var i = 0; i < keys.length; i++) {
            var prop = schema.properties[keys[i]];
            if (!prop.type) return false;
            if (simpleTypes.indexOf(prop.type) === -1) return false;
        }
        return true;
    }

    function randomId() {
        var chars = '0123456789abcdef';
        var result = 'id_';
        for (var i = 0; i < 8; i++) {
            result += chars[Math.floor(Math.random() * 16)];
        }
        return result;
    }

    // ---- editor modal ----

    function openEditor(rawMsgStr, index) {
        if (ContinueModule.isRunning()) {
            alert('Cannot edit while chat is being processed.');
            return;
        }
        try {
            editingMsg = JSON.parse(rawMsgStr);
            delete editingMsg._index;
        } catch (e) {
            editingMsg = null;
        }
        if (!editingMsg) return;
        editingIndex = index;
        toolCallRowCounter = 0;

        fetchToolSchemas().then(function () {
            buildEditorForm();
            editorOverlay.classList.remove('hidden');
        });
    }

    function closeEditor() {
        editorOverlay.classList.add('hidden');
        editingMsg = null;
        editingIndex = -1;
    }

    function buildEditorForm() {
        var msg = editingMsg;
        var title = document.getElementById('editor-title');
        title.textContent = 'Edit Message: ' + msg.role.toUpperCase() + ' (#' + editingIndex + ')';

        var html = '';

        // error banner (hidden by default)
        html += '<div id="editor-error-banner" style="display:none;padding:8px 12px;background:var(--danger);color:#fff;border-radius:4px;font-size:12px;font-weight:500"></div>';

        // send_to_server checkbox
        var sts = msg.send_to_server !== false;
        html += '<label class="editor-checkbox">';
        html += '<input type="checkbox" id="ed-send-to-server"' + (sts ? ' checked' : '') + '>';
        html += 'send_to_server';
        html += '</label>';

        // approved (assistant only)
        if (msg.role === 'assistant') {
            html += '<label class="editor-checkbox">';
            html += '<input type="checkbox" id="ed-approved"' + (msg.approved ? ' checked' : '') + '>';
            html += 'approved';
            html += '</label>';
        }

        // content
        html += '<div class="editor-section">';
        html += '<span class="editor-section-label">Content</span>';
        html += '<textarea class="editor-textarea" id="ed-content">' + Messages.escHtml(msg.content || '') + '</textarea>';
        html += '</div>';

        // tool-specific fields (always visible for tool messages)
        if (msg.role === 'tool') {
            html += '<div class="editor-section">';
            html += '<span class="editor-section-label">Tool Name</span>';
            html += '<input type="text" class="tc-name-input" id="ed-tool-name" value="' + Messages.escHtml(msg.name || '') + '" list="ed-tool-name-datalist" style="background:var(--bg-primary);color:var(--text-primary);border:1px solid var(--border);border-radius:3px;padding:3px 6px;font-family:monospace;font-size:12px">';
            html += '<datalist id="ed-tool-name-datalist"></datalist>';
            html += '</div>';
            html += '<div class="editor-section">';
            html += '<span class="editor-section-label">Tool Call ID</span>';
            html += '<input type="text" class="tc-id-input" id="ed-tool-call-id" value="' + Messages.escHtml(msg.tool_call_id || '') + '" style="background:var(--bg-primary);color:var(--text-primary);border:1px solid var(--border);border-radius:3px;padding:3px 6px;font-family:monospace;font-size:12px">';
            html += '</div>';
        }

        // assistant-specific fields
        if (msg.role === 'assistant') {
            // reasoning
            html += '<div class="editor-section">';
            html += '<span class="editor-section-label">Reasoning</span>';
            html += '<textarea class="editor-textarea" id="ed-reasoning">' + Messages.escHtml(msg.reasoning_content || '') + '</textarea>';
            html += '</div>';

            // tool calls
            html += '<div class="editor-section">';
            html += '<div class="tool-calls-section-header">';
            html += '<span class="editor-section-label">Tool Calls</span>';
            html += '<button type="button" id="editor-tc-insert">+ Insert</button>';
            html += '</div>';
            html += '<div id="ed-tool-calls-container" style="display:flex;flex-direction:column;gap:8px"></div>';
            html += '</div>';
        }

        editorBody.innerHTML = html;

        // populate tool name datalist for tool messages
        if (msg.role === 'tool') {
            var dl = document.getElementById('ed-tool-name-datalist');
            var knownTools = toolSchemaCache ? Object.keys(toolSchemaCache) : [];
            knownTools.forEach(function (tn) {
                var opt = document.createElement('option');
                opt.value = tn;
                dl.appendChild(opt);
            });
        }

        // wire up insert button for tool calls
        var tcInsert = document.getElementById('editor-tc-insert');
        if (tcInsert) {
            tcInsert.addEventListener('click', function () {
                insertToolCall();
            });
        }

        // populate tool calls
        if (msg.role === 'assistant' && msg.tool_calls && msg.tool_calls.length > 0) {
            msg.tool_calls.forEach(function (tc) {
                addToolCallRow(tc);
            });
        }

        // wire up close and save
        document.getElementById('editor-close').addEventListener('click', closeEditor);
        document.getElementById('editor-cancel').addEventListener('click', closeEditor);
        document.getElementById('editor-save').addEventListener('click', saveEdit);
    }

    // ---- tool call rows ----

    function insertToolCall() {
        var tc = {
            id: randomId(),
            function: {name: 'default_tool', arguments: '{}'}
        };
        addToolCallRow(tc);

        // update the message model
        if (!editingMsg.tool_calls) editingMsg.tool_calls = [];
        editingMsg.tool_calls.push(tc);
    }

    function addToolCallRow(tc) {
        var container = document.getElementById('ed-tool-calls-container');
        if (!container) return;
        var idx = toolCallRowCounter++;

        var row = document.createElement('div');
        row.className = 'tool-call-row';
        row.dataset.tcIndex = idx;
        row.dataset.toolCallId = tc.id;

        // build internal representation
        var tcIdx = editingMsg.tool_calls ? editingMsg.tool_calls.indexOf(tc) : -1;
        if (tcIdx === -1 && editingMsg.tool_calls) {
            tcIdx = editingMsg.tool_calls.length;
        }
        row.dataset.tcArrayIndex = tcIdx;

        // header: tool name, ID, delete
        var header = document.createElement('div');
        header.className = 'tool-call-row-header';

        var nameLabel = document.createElement('label');
        nameLabel.textContent = 'Tool:';
        var nameInput = document.createElement('input');
        nameInput.type = 'text';
        nameInput.className = 'tc-name-input';
        nameInput.value = tc.function.name || '';
        nameInput.setAttribute('list', 'tc-datalist-' + idx);

        // datalist with known tools
        var datalist = document.createElement('datalist');
        datalist.id = 'tc-datalist-' + idx;
        var knownTools = toolSchemaCache ? Object.keys(toolSchemaCache) : [];
        knownTools.forEach(function (tn) {
            var opt = document.createElement('option');
            opt.value = tn;
            datalist.appendChild(opt);
        });

        var idLabel = document.createElement('label');
        idLabel.textContent = 'ID:';
        var idInput = document.createElement('input');
        idInput.type = 'text';
        idInput.className = 'tc-id-input';
        idInput.value = tc.id || '';
        var randomBtn = document.createElement('button');
        randomBtn.type = 'button';
        randomBtn.className = 'tc-btn';
        randomBtn.textContent = 'Random';
        randomBtn.addEventListener('click', function () {
            idInput.value = randomId();
        });

        var delBtn = document.createElement('button');
        delBtn.type = 'button';
        delBtn.className = 'tc-btn danger';
        delBtn.textContent = 'Delete';
        delBtn.addEventListener('click', function () {
            removeToolCallRow(row, tc);
        });

        header.appendChild(nameLabel);
        header.appendChild(nameInput);
        header.appendChild(datalist);
        header.appendChild(idLabel);
        header.appendChild(idInput);
        header.appendChild(randomBtn);
        header.appendChild(delBtn);

        row.appendChild(header);

        // args editor
        var argsContainer = document.createElement('div');
        argsContainer.className = 'tool-call-args-editor';
        renderArgsEditor(argsContainer, tc, nameInput.value);

        // re-render args editor when tool name changes
        nameInput.addEventListener('change', function () {
            tc.function.name = this.value;
            renderArgsEditor(argsContainer, tc, this.value);
        });
        nameInput.addEventListener('blur', function () {
            tc.function.name = this.value;
            renderArgsEditor(argsContainer, tc, this.value);
        });
        idInput.addEventListener('change', function () {
            tc.id = this.value;
            row.dataset.toolCallId = this.value;
        });
        idInput.addEventListener('blur', function () {
            tc.id = this.value;
            row.dataset.toolCallId = this.value;
        });

        row.appendChild(argsContainer);
        container.appendChild(row);
    }

    function removeToolCallRow(row, tc) {
        row.remove();
        if (editingMsg.tool_calls) {
            var arrIdx = editingMsg.tool_calls.indexOf(tc);
            if (arrIdx >= 0) {
                editingMsg.tool_calls.splice(arrIdx, 1);
            }
        }
    }

    function renderArgsEditor(container, tc, toolName) {
        container.innerHTML = '';

        var schema = toolSchemaCache ? toolSchemaCache[toolName] : null;
        if (schema && isSimpleSchema(schema)) {
            renderTableArgsEditor(container, tc, schema);
        } else {
            renderRawArgsEditor(container, tc);
        }
    }

    function renderTableArgsEditor(container, tc, schema) {
        var existing = {};
        try {
            existing = JSON.parse(tc.function.arguments);
        } catch (e) {
        }

        var table = document.createElement('table');
        table.className = 'tool-call-args-table';

        var props = schema.properties;
        var required = schema.required || [];
        var keys = Object.keys(props);
        if (keys.length === 0) {
            container.innerHTML = '<span style="font-size:11px;color:var(--text-secondary)">No arguments</span>';
            return;
        }

        keys.forEach(function (key) {
            var prop = props[key];
            var row = document.createElement('tr');

            var labelCell = document.createElement('td');
            labelCell.textContent = key;
            if (required.indexOf(key) >= 0) {
                labelCell.textContent += ' *';
            }
            if (prop.description) {
                labelCell.title = prop.description;
            }

            var valueCell = document.createElement('td');

            if (prop.type === 'boolean') {
                var cb = document.createElement('input');
                cb.type = 'checkbox';
                cb.checked = !!existing[key];
                cb.dataset.field = key;
                cb.addEventListener('change', function () {
                    saveTableArgs(tc, table, schema);
                });
                valueCell.appendChild(cb);
            } else {
                var input = document.createElement('input');
                if (prop.type === 'number' || prop.type === 'integer') {
                    input.type = 'number';
                    if (prop.type === 'integer') {
                        input.step = '1';
                    }
                } else {
                    input.type = 'text';
                }
                input.dataset.field = key;
                var val = existing[key];
                if (val !== undefined && val !== null) {
                    if (typeof val === 'string' || typeof val === 'number') {
                        input.value = val;
                    } else {
                        input.value = JSON.stringify(val);
                    }
                }
                input.placeholder = prop.description || '';
                input.addEventListener('change', function () {
                    saveTableArgs(tc, table, schema);
                });
                input.addEventListener('blur', function () {
                    saveTableArgs(tc, table, schema);
                });
                valueCell.appendChild(input);
            }

            row.appendChild(labelCell);
            row.appendChild(valueCell);
            table.appendChild(row);
        });

        container.appendChild(table);
    }

    function saveTableArgs(tc, table, schema) {
        var result = {};
        table.querySelectorAll('input').forEach(function (inp) {
            var key = inp.dataset.field;
            if (!key) return;
            var prop = schema.properties[key];
            if (!prop) return;

            if (prop.type === 'boolean') {
                result[key] = inp.checked;
            } else if (prop.type === 'number' || prop.type === 'integer') {
                var num = parseFloat(inp.value);
                result[key] = isNaN(num) ? (prop.type === 'integer' ? 0 : null) : num;
            } else {
                result[key] = inp.value;
            }
        });
        tc.function.arguments = JSON.stringify(result);
    }

    function renderRawArgsEditor(container, tc) {
        var row = container.closest('.tool-call-row');
        var textarea = document.createElement('textarea');
        textarea.className = 'tool-call-raw-area';
        try {
            var obj = {
                id: tc.id,
                function: {
                    name: tc.function.name,
                    arguments: tc.function.arguments
                }
            };
            textarea.value = JSON.stringify(obj, null, 2);
        } catch (e) {
            textarea.value = JSON.stringify(tc, null, 2);
        }

        textarea.addEventListener('change', function () {
            try {
                var parsed = JSON.parse(this.value);
                tc.id = parsed.id || tc.id;
                if (row) row.dataset.toolCallId = tc.id;
                if (parsed.function) {
                    tc.function.name = parsed.function.name || tc.function.name;
                    tc.function.arguments = typeof parsed.function.arguments === 'string'
                        ? parsed.function.arguments
                        : JSON.stringify(parsed.function.arguments);
                }
                this.style.borderColor = '';
            } catch (e) {
                this.style.borderColor = 'var(--danger)';
            }
        });
        textarea.addEventListener('blur', function () {
            try {
                var parsed = JSON.parse(this.value);
                tc.id = parsed.id || tc.id;
                if (row) row.dataset.toolCallId = tc.id;
                if (parsed.function) {
                    tc.function.name = parsed.function.name || tc.function.name;
                    tc.function.arguments = typeof parsed.function.arguments === 'string'
                        ? parsed.function.arguments
                        : JSON.stringify(parsed.function.arguments);
                }
                this.style.borderColor = '';
            } catch (e) {
                this.style.borderColor = 'var(--danger)';
            }
        });

        container.appendChild(textarea);
    }

    // ---- save ----

    async function saveEdit() {
        if (!editingMsg) return;

        var saveBtn = document.getElementById('editor-save');
        saveBtn.disabled = true;
        saveBtn.textContent = 'Saving...';

        // flush any unsaved changes from inputs (blur all focused elements)
        var active = document.activeElement;
        if (active && editorOverlay.contains(active)) {
            active.blur();
        }

        // small delay to let blur handlers finish
        await new Promise(function (r) {
            setTimeout(r, 100);
        });

        // build final message object
        var msg = {};

        // send_to_server
        var stsCheck = document.getElementById('ed-send-to-server');
        msg.send_to_server = stsCheck ? stsCheck.checked : true;

        // role (immutable)
        msg.role = editingMsg.role;

        // content
        var contentEl = document.getElementById('ed-content');
        msg.content = contentEl ? contentEl.value : (editingMsg.content || '');

        // tool-specific fields
        if (editingMsg.role === 'tool') {
            var nameEl = document.getElementById('ed-tool-name');
            msg.name = nameEl ? nameEl.value.trim() : (editingMsg.name || '');
            var tcidEl = document.getElementById('ed-tool-call-id');
            msg.tool_call_id = tcidEl ? tcidEl.value.trim() : (editingMsg.tool_call_id || '');
        }

        // assistant-specific
        if (editingMsg.role === 'assistant') {
            var approvedCheck = document.getElementById('ed-approved');
            msg.approved = approvedCheck ? approvedCheck.checked : false;

            var reasoningEl = document.getElementById('ed-reasoning');
            var rc = reasoningEl ? reasoningEl.value.trim() : '';
            if (rc) {
                msg.reasoning_content = rc;
            }

            // collect tool calls from editingMsg array which has been updated live
            var rows = document.querySelectorAll('#ed-tool-calls-container .tool-call-row');
            if (rows && rows.length > 0 && editingMsg.tool_calls && editingMsg.tool_calls.length > 0) {
                msg.tool_calls = editingMsg.tool_calls.map(function (tc) {
                    return {
                        id: tc.id,
                        function: {
                            name: tc.function.name,
                            arguments: tc.function.arguments
                        }
                    };
                });
            }
        }

        var resp = await fetch('/api/chat/' + encodeURIComponent(ChatList.getCurrentTitle()) + '/message/' + editingIndex, {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(msg)
        });

        saveBtn.disabled = false;
        saveBtn.textContent = 'Save';

        if (resp.ok) {
            closeEditor();
            await ChatList.loadMessages();
        } else {
            var data = await resp.json();
            if (data.errors) {
                Messages.highlightErrors(data.errors);
                var banner = document.getElementById('editor-error-banner');
                if (banner) {
                    var ids = data.errors.map(function (e) {
                        return e.tool_call_id;
                    }).filter(Boolean);
                    var idxs = data.errors.map(function (e) {
                        return e.message_index;
                    }).filter(function (v) {
                        return v !== undefined && v >= 0;
                    });
                    var parts = [];
                    if (idxs.length > 0) parts.push('Messages: ' + idxs.join(', '));
                    if (ids.length > 0) parts.push('Tool calls: ' + ids.join(', '));
                    banner.textContent = 'Validation failed: ' + (parts.length > 0 ? parts.join('; ') : 'Unknown issue');
                    banner.style.display = 'block';
                    banner.classList.remove('highlight-error');
                    void banner.offsetWidth;
                    banner.classList.add('highlight-error');
                }
                // highlight matching tool call rows in the editor
                data.errors.forEach(function (err) {
                    if (err.tool_call_id) {
                        var row = document.querySelector('#ed-tool-calls-container .tool-call-row[data-tool-call-id="' + err.tool_call_id + '"]');
                        if (row) {
                            row.classList.remove('highlight-error');
                            void row.offsetWidth;
                            row.classList.add('highlight-error');
                        }
                    }
                });
            } else {
                alert('Save failed: ' + (data.error || 'Unknown'));
            }
        }
    }

    // ---- event delegation for message buttons ----

    messagesContainer.addEventListener('click', function (e) {
        var emptyBtn = e.target.closest('.btn-insert-empty');
        if (emptyBtn) {
            startInsert(0);
            return;
        }

        var approveBtn = e.target.closest('.btn-approve-msg');
        if (approveBtn) {
            var msgEl = approveBtn.closest('.message');
            var idx = parseInt(msgEl.dataset.index);
            if (!isNaN(idx)) toggleApprove(idx, approveBtn);
            return;
        }

        var copyBtn = e.target.closest('.btn-copy-msg');
        if (copyBtn) {
            var msgEl = copyBtn.closest('.message');
            if (msgEl && msgEl.dataset.rawMessage) {
                try {
                    var msg = JSON.parse(msgEl.dataset.rawMessage);
                    if (msg.content) {
                        navigator.clipboard.writeText(msg.content).then(function () {
                            copyBtn.textContent = 'Copied';
                            setTimeout(function () {
                                copyBtn.textContent = 'Copy';
                            }, 1500);
                        }).catch(function () {
                            // fallback: select from the content element
                            var area = document.createElement('textarea');
                            area.value = msg.content;
                            area.style.position = 'fixed';
                            area.style.left = '-9999px';
                            document.body.appendChild(area);
                            area.select();
                            document.execCommand('copy');
                            document.body.removeChild(area);
                            copyBtn.textContent = 'Copied';
                            setTimeout(function () {
                                copyBtn.textContent = 'Copy';
                            }, 1500);
                        });
                    }
                } catch (ex) {
                }
            }
            return;
        }

        var btn = e.target.closest('.msg-actions button');
        if (!btn) return;

        var msgEl = btn.closest('.message');
        var index = parseInt(msgEl.dataset.index);
        if (isNaN(index)) return;

        if (btn.classList.contains('btn-edit-msg')) {
            var rawMsg = msgEl.dataset.rawMessage;
            if (rawMsg) openEditor(rawMsg, index);
        } else if (btn.classList.contains('btn-del-msg')) {
            deleteMessage(msgEl, index);
        } else if (btn.classList.contains('btn-insb-msg')) {
            startInsert(index);
        } else if (btn.classList.contains('btn-insa-msg')) {
            startInsert(index + 1);
        }
    });

    // ---- insert (kept in-place for now) ----

    function startInsert(index) {
        if (ContinueModule.isRunning()) {
            alert('Cannot insert while chat is being processed.');
            return;
        }
        var container = document.getElementById('messages');
        var emptyBtn = container.querySelector('.empty-state');
        if (emptyBtn) {
            emptyBtn.remove();
        }

        var div = document.createElement('div');
        div.className = 'message role-user editing';
        div.style.cssText = 'align-self:flex-start;max-width:85%';

        var roleRow = document.createElement('div');
        roleRow.style.cssText = 'display:flex;align-items:center;gap:8px;margin-bottom:6px';
        var roleLabel = document.createElement('span');
        roleLabel.textContent = 'Role:';
        roleLabel.style.cssText = 'color:var(--text-secondary);font-size:13px';
        var roleSelect = document.createElement('select');
        roleSelect.style.cssText = 'background:var(--bg-primary);color:var(--text-primary);border:1px solid var(--accent);padding:4px 8px;border-radius:4px;font-size:13px';
        var roles = ['user', 'system', 'assistant', 'tool'];
        roles.forEach(function (r) {
            var opt = document.createElement('option');
            opt.value = r;
            opt.textContent = r;
            if (r === 'user') opt.selected = true;
            roleSelect.appendChild(opt);
        });
        roleRow.appendChild(roleLabel);
        roleRow.appendChild(roleSelect);
        div.appendChild(roleRow);

        var textarea = document.createElement('textarea');
        textarea.placeholder = 'Enter content for new message...';
        textarea.style.cssText = 'width:100%;min-height:80px;background:var(--bg-primary);color:var(--text-primary);border:1px solid var(--accent);padding:8px;border-radius:4px;font-family:inherit;font-size:14px;resize:vertical;';
        div.appendChild(textarea);

        let save = async function () {
            var content = textarea.value.trim();
            if (!content) {
                div.remove();
                return;
            }

            var resp = await fetch('/api/chat/' + encodeURIComponent(ChatList.getCurrentTitle()) + '/message/' + index, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({role: roleSelect.value, content: content, send_to_server: true})
            });

            var data = await resp.json();
            if (resp.ok && !data.errors) {
                await ChatList.loadMessages();
            } else if (data.errors) {
                Messages.highlightErrors(data.errors);
                div.classList.add('highlight-error');
                setTimeout(function () {
                    div.classList.remove('highlight-error');
                }, 5000);
                var prevErr = div.querySelector('.insert-error');
                if (prevErr) prevErr.remove();
                var errEl = document.createElement('div');
                errEl.className = 'insert-error';
                errEl.style.cssText = 'color:var(--danger);font-size:11px;margin-top:4px';
                errEl.textContent = data.errors.map(function (e) {
                    return e.detail;
                }).join('; ');
                btnRow.parentNode.insertBefore(errEl, btnRow);
            } else {
                alert('Insert failed: ' + (data.error || 'Unknown'));
            }
        };

        var btnRow = document.createElement('div');
        btnRow.style.cssText = 'display:flex;gap:8px;margin-top:8px;justify-content:flex-end';
        var cancelBtn = document.createElement('button');
        cancelBtn.type = 'button';
        cancelBtn.textContent = 'Cancel';
        cancelBtn.className = 'btn-cancel';
        cancelBtn.style.cssText = 'padding:4px 12px;background:var(--bg-secondary);color:var(--text-primary);border:1px solid var(--border);border-radius:4px;cursor:pointer;font-size:12px';
        cancelBtn.addEventListener('click', function () {
            div.remove();
        });
        var saveBtn = document.createElement('button');
        saveBtn.type = 'button';
        saveBtn.textContent = 'Save';
        saveBtn.className = 'btn-save';
        saveBtn.style.cssText = 'padding:4px 12px;background:var(--accent);color:#fff;border:none;border-radius:4px;cursor:pointer;font-size:12px';
        saveBtn.addEventListener('click', save);
        btnRow.appendChild(cancelBtn);
        btnRow.appendChild(saveBtn);
        div.appendChild(btnRow);

        textarea.addEventListener('keydown', function (e) {
            if (e.key === 'Enter' && !e.shiftKey) {
                e.preventDefault();
                save();
            }
        });

        var msgEls = container.querySelectorAll('.message');
        if (msgEls.length > 0 && index < msgEls.length) {
            container.insertBefore(div, msgEls[index]);
        } else {
            container.appendChild(div);
        }
        textarea.focus();
    }

    async function toggleApprove(index, btn) {
        if (ContinueModule.isRunning()) {
            alert('Cannot approve while chat is being processed.');
            return;
        }
        var resp = await fetch('/api/chat/' + encodeURIComponent(ChatList.getCurrentTitle()) + '/message/' + index + '/approve', {
            method: 'PUT'
        });
        if (resp.ok) {
            var data = await resp.json();
            btn.textContent = data.approved ? 'Unapprove' : 'Approve';
            await ChatList.loadMessages();
        } else {
            var data = await resp.json();
            alert('Approve failed: ' + (data.error || 'Unknown'));
        }
    }

    // ---- delete ----

    async function deleteMessage(msgEl, index) {
        if (ContinueModule.isRunning()) {
            alert('Cannot delete while chat is being processed.');
            return;
        }
        if (!window.isSkipConfirm || !window.isSkipConfirm()) {
            if (!confirm('Delete this message?')) return;
        }

        var resp = await fetch('/api/chat/' + encodeURIComponent(ChatList.getCurrentTitle()) + '/message/' + index, {
            method: 'DELETE'
        });

        if (resp.ok) {
            await ChatList.loadMessages();
        } else {
            var data = await resp.json();
            alert('Delete failed: ' + (data.error || 'Unknown'));
        }
    }

})();
