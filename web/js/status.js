// status.js - global chat running-state subscription
//
// Protocol:
//   GET /api/chats/status  (SSE, EventSource)
//     event: status  {"running": {"title": true, ...}} - full snapshot,
//     sent on connect and on every running-state change of any chat.
//
// The per-chat stream (/api/chat/stream) only covers the currently selected
// chat, so it cannot keep the sidebar markers of background chats up to
// date. This module subscribes once per page and incrementally adds/removes
// the ".chat-running" marker on the corresponding #chat-list <li>.
(function () {
    'use strict';

    var runningTitles = {};
    var hasSnapshot = false; // don't wipe list-rendered markers before the first snapshot
    var offlineGraceTimer = null;

    // ---- backend connectivity marker ----
    // The status SSE doubles as a liveness probe: EventSource fires "error"
    // (readyState CONNECTING) while it is reconnecting after a connection
    // loss, and "open" once the backend is reachable again.

    function setOfflineMarkerVisible(visible) {
        var el = document.getElementById('backend-offline');
        if (el) el.classList.toggle('hidden', !visible);
    }

    function noteOpen() {
        if (offlineGraceTimer) {
            clearTimeout(offlineGraceTimer);
            offlineGraceTimer = null;
        }
        setOfflineMarkerVisible(false);
    }

    function noteError(es) {
        if (es.readyState === EventSource.OPEN || offlineGraceTimer) return;
        // grace period so a fast reconnect doesn't flash the marker
        offlineGraceTimer = setTimeout(function () {
            offlineGraceTimer = null;
            if (es.readyState !== EventSource.OPEN) {
                setOfflineMarkerVisible(true);
            }
        }, 1500);
    }

    function applyMarkers() {
        if (!hasSnapshot) return;
        document.querySelectorAll('#chat-list li').forEach(function (li) {
            var title = li.dataset.title;
            if (!title) return;
            var marker = li.querySelector('.chat-running');
            if (runningTitles[title]) {
                if (!marker) {
                    var titleEl = li.querySelector('.chat-title');
                    marker = document.createElement('span');
                    marker.className = 'chat-running';
                    marker.title = 'Generating...';
                    marker.textContent = '●';
                    if (titleEl && titleEl.nextSibling) {
                        li.insertBefore(marker, titleEl.nextSibling);
                    } else {
                        li.appendChild(marker);
                    }
                }
            } else if (marker) {
                marker.remove();
            }
        });
    }

    function connect() {
        var es = new EventSource('/api/chats/status');
        es.onopen = noteOpen;
        es.onerror = function () {
            noteError(es);
        };
        es.addEventListener('status', function (e) {
            var d;
            try {
                d = JSON.parse(e.data);
            } catch (err) {
                return;
            }
            runningTitles = d.running || {};
            hasSnapshot = true;
            applyMarkers();
            // The status stream also reconnects after a backend restart; keep
            // the global Read/Write/Sudo mode UI truthful as well.
            if (window.DsApp && window.DsApp.refreshMode) {
                window.DsApp.refreshMode();
            }
        });
        // EventSource auto-reconnects; the server resends a full snapshot
        // on every (re)connect, so missed transitions are self-healing.
    }

    window.ChatStatus = {
        isRunning: function (title) {
            return hasSnapshot && !!runningTitles[title];
        },
        applyMarkers: applyMarkers
    };

    connect();
})();
