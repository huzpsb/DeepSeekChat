// tools.js - tool management modal
(function () {
    'use strict';
    var modal = document.getElementById('modal-overlay');
    var modalBody = document.getElementById('modal-body');

    document.getElementById('btn-tools').addEventListener('click', loadTools);
    document.getElementById('modal-close').addEventListener('click', function () {
        modal.classList.add('hidden');
    });
    modal.addEventListener('click', function (e) {
        if (e.target === modal) modal.classList.add('hidden');
    });

    async function loadTools() {
        modal.classList.remove('hidden');
        modalBody.innerHTML = '<div style="padding:20px;text-align:center">Loading...</div>';
        try {
            var resp = await fetch('/api/mcp/tools');
            var tools = await resp.json();
            renderTools(tools);
        } catch (e) {
            modalBody.innerHTML = '<div style="color:var(--danger);padding:20px">Failed to load tools</div>';
        }
    }

    function renderTools(tools) {
        if (!tools || tools.length === 0) {
            modalBody.innerHTML = '<div style="padding:20px;text-align:center;color:var(--text-secondary)">No tools connected</div>';
            return;
        }

        var groups = {};
        tools.forEach(function (t) {
            if (!groups[t.mcp_name]) groups[t.mcp_name] = [];
            groups[t.mcp_name].push(t);
        });

        var html = '';

        Object.keys(groups).forEach(function (mcpName) {
            html += '<div style="margin-bottom:16px">';
            html += '<h4 style="color:var(--accent);margin-bottom:8px">' + Messages.escHtml(mcpName) + '</h4>';
            groups[mcpName].forEach(function (tool) {
                var fullName = tool.mcp_name + '::' + tool.tool_name;
                var isAskUser = tool.mcp_name === 'AskUser' && tool.tool_name === 'ask_user';
                var statusClass = '';
                if (tool.status === 'approved') statusClass = 'style="color:var(--success)"';
                else if (tool.status === 'manually_approved') statusClass = 'style="color:var(--warning)"';

                html += '<div class="tool-row" data-fullname="' + Messages.escHtml(fullName) + '" style="display:flex;align-items:center;gap:8px;padding:6px 8px;border-bottom:1px solid var(--border)">';
                html += '<span style="flex:1">' + Messages.escHtml(tool.tool_name) + '</span>';
                if (!tool.available) {
                    html += '<span style="font-size:11px;color:var(--text-secondary)">(disconnected)</span>';
                }
                html += '<select class="tool-status-select" style="background:var(--bg-primary);color:var(--text-primary);border:1px solid var(--border);padding:2px 6px;border-radius:3px;font-size:12px">';
                html += '<option value="approved" ' + (tool.status === 'approved' ? 'selected' : '') + (isAskUser ? ' disabled' : '') + '>Approved</option>';
                html += '<option value="manually_approved" ' + (tool.status === 'manually_approved' ? 'selected' : '') + '>Manual</option>';
                html += '<option value="unapproved" ' + (tool.status === 'unapproved' ? 'selected' : '') + '>Unapproved</option>';
                html += '</select>';
                html += '</div>';
            });
            html += '</div>';
            if (Object.keys(groups).length > 1) {
                html += '<div style="text-align:right;margin-top:4px">';
                html += '<button onclick="this.closest(\'div\').previousElementSibling.querySelectorAll(\'.tool-status-select\').forEach(function(s){if(!s.querySelector(\'option[value=approved]\').disabled){s.value=\'approved\';s.dispatchEvent(new Event(\'change\'))}})" style="font-size:11px;background:var(--bg-tertiary);color:var(--text-primary);border:1px solid var(--border);padding:2px 8px;border-radius:3px;cursor:pointer">All Approved</button> ';
                html += '<button onclick="this.closest(\'div\').previousElementSibling.querySelectorAll(\'.tool-status-select\').forEach(function(s){s.value=\'manually_approved\';s.dispatchEvent(new Event(\'change\'))})" style="font-size:11px;background:var(--bg-tertiary);color:var(--text-primary);border:1px solid var(--border);padding:2px 8px;border-radius:3px;cursor:pointer">All Manual</button>';
                html += '</div>';
            }
        });

        html += '<div style="margin-top:12px;text-align:right">';
        html += '<button id="btn-reload-mcp" style="background:var(--bg-tertiary);color:var(--text-primary);border:1px solid var(--border);padding:4px 12px;border-radius:4px;cursor:pointer;font-size:12px">Reload MCP</button>';
        html += '</div>';

        modalBody.innerHTML = html;

        modalBody.querySelectorAll('.tool-status-select').forEach(function (sel) {
            sel.addEventListener('change', async function () {
                var row = this.closest('.tool-row');
                var fullName = row.dataset.fullname;
                var status = this.value;
                if (fullName === 'AskUser::ask_user' && status === 'approved') {
                    this.value = 'unapproved';
                    status = 'unapproved';
                }
                var update = {};
                update[fullName] = status;
                try {
                    await fetch('/api/mcp/tools', {
                        method: 'PUT',
                        headers: {'Content-Type': 'application/json'},
                        body: JSON.stringify(update)
                    });
                } catch (e) {
                    console.error('Failed to update tool status:', e);
                }
            });
        });

        var reloadBtn = document.getElementById('btn-reload-mcp');
        if (reloadBtn) {
            // WebStorm is too dumb to infer this -> button :(
            reloadBtn.addEventListener('click', async function () {
                // noinspection JSUnusedGlobalSymbols
                this.disabled = true;
                this.textContent = 'Reloading...';
                try {
                    await fetch('/api/mcp/reload', {method: 'POST'});
                    await loadTools();
                } catch (e) {
                    this.textContent = 'Failed';
                }
                // noinspection JSUnusedGlobalSymbols
                this.disabled = false;
                this.textContent = 'Reload MCP';
            });
        }
    }
})();
