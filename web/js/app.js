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
        document.getElementById('pref-root-dir').value = backendConfig.root_dir || '';
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
        document.getElementById('pref-root-dir').addEventListener('change', function () {
            var dir = this.value.trim();
            if (dir) saveBackendSettings({root_dir: dir});
        });
        document.getElementById('pref-provider').addEventListener('change', function () {
            saveBackendSettings({provider: this.value});
        });
        document.getElementById('pref-model').addEventListener('change', function () {
            saveBackendSettings({model: this.value});
        });
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
