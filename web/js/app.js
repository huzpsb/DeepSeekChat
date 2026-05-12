// app.js - main controller
(function () {
    'use strict';
    var currentMode = 'readonly';

    window.DsApp = {};

    window.DsApp.getMode = function () {
        return currentMode;
    };

    async function init() {
        try {
            var resp = await fetch('/api/mode');
            var data = await resp.json();
            currentMode = data.mode;
            updateModeUI();
        } catch (e) {
            console.error('Failed to load mode:', e);
        }

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

        if (ChatList.getCurrentTitle()) {
            await ChatList.loadMessages();
        }
    });
})();
