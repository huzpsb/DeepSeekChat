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
        if (currentMode === 'readonly') {
            label.style.display = '';
        } else {
            label.style.display = 'none';
            cb.checked = false;
            localStorage.removeItem('noob_mode');
        }
        applyNoobModeUI();
    }

    function initNoobMode() {
        var cb = document.getElementById('noob-mode');
        var label = document.getElementById('noob-mode-label');
        if (currentMode === 'readonly') {
            label.style.display = '';
            cb.checked = localStorage.getItem('noob_mode') === 'true';
        }
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
        initSkipConfirm();
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

    document.getElementById('btn-export').addEventListener('click', function () {
        Messages.exportToHtml();
    });
})();
