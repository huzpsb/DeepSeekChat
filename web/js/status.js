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
    var es = null;
    var retryTimer = null;

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

    // CLOSED is terminal: the server ANSWERED the stream request with a
    // non-200 response — network-level failures keep readyState CONNECTING
    // with the browser retrying on its own. On this endpoint a non-200 is
    // the auth gate's 401 JSON (e.g. the cookie died when the backend
    // restarted with a fresh salt or the password changed), and a plain
    // "backend offline" would misdiagnose it. EventSource hides the status
    // code, so confirm over fetch — a 401 there trips the global wrapper
    // in app.js, which bounces the tab to the login page. The CLOSED
    // stream never reconnects by itself, so also schedule a fresh
    // connect(); without it the "reconnecting…" marker would be an empty
    // promise for every other non-200 too (e.g. a proxy's 502 while the
    // backend restarts).
    function onFatal() {
        fetch('api/mode').catch(function () {
            // backend unreachable, or the tab is already navigating to
            // the login page via the wrapper — nothing to do here.
        });
        if (!retryTimer) {
            retryTimer = setTimeout(function () {
                retryTimer = null;
                connect();
            }, 3000);
        }
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
        if (retryTimer) {
            clearTimeout(retryTimer);
            retryTimer = null;
        }
        if (es) {
            es.close(); // replace, don't accumulate, EventSources
            es = null;
        }
        es = new EventSource('api/chats/status');
        es.onopen = noteOpen;
        es.onerror = function () {
            noteError(es);
            if (es.readyState === EventSource.CLOSED) {
                onFatal();
            }
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
