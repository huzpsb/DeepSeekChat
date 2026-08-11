// app.js - main controller
(function () {
    'use strict';
    var currentMode = 'readonly';

    window.DsApp = {};

    window.DsApp.getMode = function () {
        return currentMode;
    };

    window.NoobMode = {
        isActive: function () {
            return localStorage.getItem('noob_mode') === 'true'
                && currentMode === 'readonly';
        }
    };

    function applyNoobModeUI() {
        var noobActive = window.NoobMode.isActive();
        document.getElementById('mode-controls').style.display = noobActive ? 'none' : '';
        document.getElementById('btn-continue').disabled = noobActive && (window.ContinueModule ? window.ContinueModule.isRunning() : false);
    }

    function handleModeChangeForNoob() {
        var label = document.getElementById('noob-mode-label');
        var cb = document.getElementById('noob-mode');
        var disabled = currentMode !== 'readonly';
        cb.disabled = disabled;
        label.classList.toggle('disabled', disabled);
        applyNoobModeUI();
    }

    function initNoobMode() {
        var cb = document.getElementById('noob-mode');
        cb.checked = localStorage.getItem('noob_mode') === 'true';
        handleModeChangeForNoob();
        applyNoobModeUI();

        cb.addEventListener('change', function () {
            if (cb.checked) {
                localStorage.setItem('noob_mode', 'true');
            } else {
                localStorage.removeItem('noob_mode');
            }
            applyNoobModeUI();
            if (ChatList.getCurrentTitle()) {
                ChatList.loadMessages();
            }
        });
    }

    async function init() {
        try {
            var resp = await fetch('/api/mode');
            var data = await resp.json();
            currentMode = data.mode;
            updateModeUI();
        } catch (e) {
            console.error('Failed to load mode:', e);
        }

        initNoobMode();
        initPreferencesModal();
        initSkipConfirm();
        initStopSoundSettings();
        await loadBackendSettings();
        ChatList.init();
    }

    function updateModeUI() {
        document.querySelectorAll('.mode-btn').forEach(function (btn) {
            btn.classList.toggle('active', btn.dataset.mode === currentMode);
        });
    }

    var versionOverlay = document.getElementById('version-overlay');

    document.getElementById('btn-version').addEventListener('click', function () {
        versionOverlay.classList.remove('hidden');
    });

    document.getElementById('version-close').addEventListener('click', function () {
        versionOverlay.classList.add('hidden');
    });

    versionOverlay.addEventListener('click', function (e) {
        if (e.target === versionOverlay) {
            versionOverlay.classList.add('hidden');
        }
    });

    function initPreferencesModal() {
        var overlay = document.getElementById('preferences-overlay');
        document.getElementById('btn-preferences').addEventListener('click', function () {
            overlay.classList.remove('hidden');
            loadBackendSettings();
        });
        document.getElementById('preferences-close').addEventListener('click', function () {
            overlay.classList.add('hidden');
        });
        overlay.addEventListener('click', function (e) {
            if (e.target === overlay) {
                overlay.classList.add('hidden');
            }
        });
        initBackendSettings();
    }

    var backendConfig = null;

    async function loadBackendSettings() {
        try {
            var resp = await fetch('/api/config');
            backendConfig = await resp.json();
            renderBackendSettings();
        } catch (e) {
            console.error('Failed to load config:', e);
        }
    }

    function renderBackendSettings() {
        if (!backendConfig) return;
        renderRootDirList();
        renderRootDirSelector();
        var providerSel = document.getElementById('pref-provider');
        providerSel.innerHTML = '';
        (backendConfig.providers || []).forEach(function (p) {
            var opt = document.createElement('option');
            opt.value = p.name;
            opt.textContent = p.name;
            if (p.name === backendConfig.provider) opt.selected = true;
            providerSel.appendChild(opt);
        });
        renderModelOptions();
    }

    function rootDirs() {
        return (backendConfig && backendConfig.root_dirs) || [];
    }

    function renderRootDirList() {
        var box = document.getElementById('pref-root-dirs');
        box.innerHTML = '';
        rootDirs().forEach(function (dir) {
            var row = document.createElement('div');
            row.className = 'pref-root-dir-item';
            var label = document.createElement('span');
            label.textContent = dir;
            var del = document.createElement('button');
            del.textContent = '×';
            del.title = 'Remove';
            del.addEventListener('click', function () {
                removeRootDir(dir);
            });
            row.appendChild(label);
            row.appendChild(del);
            box.appendChild(row);
        });
    }

    function renderRootDirSelector() {
        var sel = document.getElementById('root-dir-select');
        var current = sel.value;
        sel.innerHTML = '';
        rootDirs().forEach(function (dir) {
            var opt = document.createElement('option');
            opt.value = dir;
            opt.textContent = dir;
            sel.appendChild(opt);
        });
        if (current && rootDirs().indexOf(current) >= 0) {
            sel.value = current;
        }
        sel.disabled = !ChatList.getCurrentTitle() || rootDirs().length === 0;
    }

    async function addRootDir(dir) {
        if (!dir || rootDirs().indexOf(dir) >= 0) return;
        await saveBackendSettings({root_dirs: rootDirs().concat([dir])});
    }

    async function removeRootDir(dir) {
        var dirs = rootDirs().filter(function (d) {
            return d !== dir;
        });
        if (dirs.length === 0) {
            alert('At least one root dir is required.');
            return;
        }
        await saveBackendSettings({root_dirs: dirs});
    }

    // DsApp.updateRootDirSelector is called by chat.js whenever the
    // current chat (or its stored root dir) changes.
    window.DsApp.updateRootDirSelector = function (chatRootDir) {
        var sel = document.getElementById('root-dir-select');
        var dirs = rootDirs();
        if (chatRootDir && dirs.indexOf(chatRootDir) >= 0) {
            sel.value = chatRootDir;
        } else if (dirs.length > 0) {
            sel.value = dirs[0];
        }
        sel.dataset.chatRootDir = sel.value;
        sel.disabled = !ChatList.getCurrentTitle() || dirs.length === 0;
    };

    async function switchChatRootDir(dir) {
        var title = ChatList.getCurrentTitle();
        var sel = document.getElementById('root-dir-select');
        if (!title) return;
        var resp = await fetch('/api/chats/' + encodeURIComponent(title) + '/rootdir', {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({root_dir: dir})
        });
        if (!resp.ok) {
            var err = await resp.json();
            alert('Switch root dir failed: ' + (err.error || 'Unknown'));
            window.DsApp.updateRootDirSelector(sel.dataset.chatRootDir || '');
            return;
        }
        sel.dataset.chatRootDir = dir;
    }

    function initRootDirSelector() {
        document.getElementById('root-dir-select').addEventListener('change', function () {
            switchChatRootDir(this.value);
        });
    }

    function renderModelOptions() {
        var modelSel = document.getElementById('pref-model');
        modelSel.innerHTML = '';
        var providerName = document.getElementById('pref-provider').value;
        (backendConfig.providers || []).forEach(function (p) {
            if (p.name !== providerName) return;
            (p.models || []).forEach(function (m) {
                var opt = document.createElement('option');
                opt.value = m;
                opt.textContent = m;
                if (m === backendConfig.model && p.name === backendConfig.provider) opt.selected = true;
                modelSel.appendChild(opt);
            });
        });
    }

    async function saveBackendSettings(update) {
        try {
            var resp = await fetch('/api/config', {
                method: 'PUT',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(update)
            });
            if (!resp.ok) {
                var err = await resp.json();
                console.error('Failed to save config:', err.error);
                return;
            }
            backendConfig = await resp.json();
            renderBackendSettings();
        } catch (e) {
            console.error('Failed to save config:', e);
        }
    }

    function initBackendSettings() {
        document.getElementById('pref-root-dir-add').addEventListener('click', function () {
            var input = document.getElementById('pref-root-dir-new');
            var dir = input.value.trim();
            if (dir) {
                input.value = '';
                addRootDir(dir);
            }
        });
        document.getElementById('pref-root-dir-new').addEventListener('keydown', function (e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                document.getElementById('pref-root-dir-add').click();
            }
        });
        document.getElementById('pref-provider').addEventListener('change', function () {
            saveBackendSettings({provider: this.value});
        });
        document.getElementById('pref-model').addEventListener('change', function () {
            saveBackendSettings({model: this.value});
        });
        initRootDirSelector();
    }

    document.addEventListener('DOMContentLoaded', init);

    document.getElementById('mode-controls').addEventListener('click', async function (e) {
        var btn = e.target.closest('.mode-btn');
        if (!btn) return;
        var mode = btn.dataset.mode;
        await fetch('/api/mode', {
            method: 'PUT',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({mode: mode})
        });
        currentMode = mode;
        updateModeUI();
        handleModeChangeForNoob();

        if (ChatList.getCurrentTitle()) {
            await ChatList.loadMessages();
        }
    });

    function initSkipConfirm() {
        var cb = document.getElementById('skip-confirm');
        if (cb) {
            cb.checked = localStorage.getItem('skip_confirm') === 'true';
            cb.addEventListener('change', function () {
                if (cb.checked) {
                    localStorage.setItem('skip_confirm', 'true');
                } else {
                    localStorage.removeItem('skip_confirm');
                }
            });
        }
    }

    window.isSkipConfirm = function () {
        return localStorage.getItem('skip_confirm') === 'true';
    };

    function initStopSoundSettings() {
        bindStoredCheckbox('stop-sound', 'stop_sound');
        bindStoredCheckbox('loop-sound', 'loop_stop_sound');
    }

    function bindStoredCheckbox(id, key) {
        var cb = document.getElementById(id);
        if (!cb) return;
        cb.checked = localStorage.getItem(key) === 'true';
        cb.addEventListener('change', function () {
            if (cb.checked) {
                localStorage.setItem(key, 'true');
            } else {
                localStorage.removeItem(key);
            }
            if (window.AskUserPrompt && window.AskUserPrompt.updateMuteButton) {
                window.AskUserPrompt.updateMuteButton();
            }
        });
    }

    document.getElementById('btn-export').addEventListener('click', function () {
        Messages.exportToHtml();
    });
})();
