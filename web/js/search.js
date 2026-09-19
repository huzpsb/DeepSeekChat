// search.js - full-text chat search (sidebar)
//
// GET /api/chats/search?q=<text>&tool=<0|1>
//   → { results: [{ title, title_match, total_hits, truncated,
//                   hits: [{ index, role, field, count, snippet }] }] }
//
// The query matches message content + reasoning (case-insensitive); the
// "tool" toggle additionally covers tool calls and tool results. The
// server caps each chat at 10 occurrences, keeping the later ones.
(function () {
    'use strict';

    var input, toolBtn, resultsEl, listEl;
    var debounceTimer = null;
    var includeTool = false;
    var seq = 0; // guards against out-of-order responses

    var DEBOUNCE_MS = 250;

    function init() {
        input = document.getElementById('chat-search');
        toolBtn = document.getElementById('search-toggle-tool');
        resultsEl = document.getElementById('search-results');
        listEl = document.getElementById('chat-list');
        if (!input || !toolBtn || !resultsEl) return;

        input.addEventListener('input', function () {
            clearTimeout(debounceTimer);
            debounceTimer = setTimeout(runSearch, DEBOUNCE_MS);
        });
        input.addEventListener('keydown', function (e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                clearTimeout(debounceTimer);
                runSearch();
            }
            if (e.key === 'Escape') {
                input.value = '';
                clearResults();
            }
        });
        toolBtn.addEventListener('click', function () {
            includeTool = !includeTool;
            toolBtn.classList.toggle('active', includeTool);
            clearTimeout(debounceTimer);
            runSearch();
        });
    }

    function clearResults() {
        seq++; // drop any in-flight response
        resultsEl.classList.add('hidden');
        resultsEl.innerHTML = '';
        listEl.style.display = '';
    }

    async function runSearch() {
        var q = input.value.trim();
        if (!q) {
            clearResults();
            return;
        }
        var mySeq = ++seq;
        try {
            var url = 'api/chats/search?q=' + encodeURIComponent(q) + '&tool=' + (includeTool ? '1' : '0');
            var resp = await fetch(url);
            if (!resp.ok) return;
            var data = await resp.json();
            if (mySeq !== seq) return; // stale response
            renderResults(q, data.results || []);
        } catch (e) {
            console.error('Chat search failed:', e);
        }
    }

    function esc(s) {
        return String(s).replace(/[&<>"']/g, function (c) {
            return {'&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;'}[c];
        });
    }

    // Highlight query occurrences inside an already HTML-escaped snippet.
    function highlight(snippet, q) {
        var escaped = esc(snippet);
        var needle = esc(q).replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        try {
            return escaped.replace(new RegExp(needle, 'gi'), function (m) {
                return '<mark>' + m + '</mark>';
            });
        } catch (e) {
            return escaped;
        }
    }

    function hitMeta(h) {
        var parts = ['#' + h.index + ' ' + String(h.role || '').toUpperCase()];
        if (h.field && h.field !== 'content') parts.push(h.field.replace(/_/g, ' '));
        if (h.count > 1) parts.push('×' + h.count);
        return parts.join(' · ');
    }

    function renderResults(q, results) {
        resultsEl.innerHTML = '';
        if (results.length === 0) {
            resultsEl.innerHTML = '<div class="search-empty">No matches for “' + esc(q) + '”</div>';
        }
        results.forEach(function (r) {
            var group = document.createElement('div');
            group.className = 'search-group';

            var head = document.createElement('div');
            head.className = 'search-group-head';
            var title = document.createElement('span');
            title.className = 'search-group-title';
            title.textContent = r.title;
            if (r.title_match) title.classList.add('title-match');
            title.title = r.title;
            var count = document.createElement('span');
            count.className = 'search-group-count';
            count.textContent = r.total_hits === 0
                ? 'title match'
                : r.total_hits + (r.truncated ? ' (last ' + (r.hits || []).length + ' shown)' : '') + ' hit' + (r.total_hits === 1 ? '' : 's');
            head.appendChild(title);
            head.appendChild(count);
            head.addEventListener('click', function () {
                ChatList.select(r.title);
            });
            group.appendChild(head);

            (r.hits || []).forEach(function (h) {
                var row = document.createElement('div');
                row.className = 'search-hit';
                var meta = document.createElement('span');
                meta.className = 'search-hit-meta';
                meta.textContent = hitMeta(h);
                var snippet = document.createElement('span');
                snippet.className = 'search-hit-snippet';
                snippet.innerHTML = highlight(h.snippet, q);
                row.appendChild(meta);
                row.appendChild(snippet);
                row.addEventListener('click', function () {
                    jumpTo(r.title, h.index);
                });
                group.appendChild(row);
            });

            resultsEl.appendChild(group);
        });
        resultsEl.classList.remove('hidden');
        listEl.style.display = 'none';
    }

    // Open the chat, then scroll to the message and flash it. Collapsed
    // tool results / reasoning / tool calls are expanded so the hit is
    // actually visible.
    function jumpTo(title, index) {
        ChatList.select(title).then(function () {
            setTimeout(function () {
                var el = document.querySelector('#messages .message[data-index="' + index + '"]');
                if (!el) return; // e.g. hidden by noob mode
                expandCollapsed(el);
                el.scrollIntoView({block: 'center', behavior: 'smooth'});
                el.classList.remove('highlight-success');
                void el.offsetWidth; // restart the CSS animation
                el.classList.add('highlight-success');
                setTimeout(function () {
                    el.classList.remove('highlight-success');
                }, 5000);
            }, 50);
        });
    }

    function expandCollapsed(el) {
        // reasoning and tool-call blocks: the toggle is directly followed
        // by its collapsible content
        el.querySelectorAll('.reasoning-toggle, .tool-calls-toggle').forEach(function (toggle) {
            var target = toggle.nextElementSibling;
            if (target && target.style.display === 'none') {
                toggle.click();
            }
        });
        // tool results are different: the toggle is the LAST child of the
        // header (nextElementSibling is null), the collapsed .msg-content
        // is the header's next sibling instead
        var toolToggle = el.querySelector('.tool-result-toggle');
        if (toolToggle) {
            var header = toolToggle.closest('.msg-header');
            var target = header ? header.nextElementSibling : null;
            if (target && target.style.display === 'none') {
                toolToggle.click();
            }
        }
    }

    window.ChatSearch = {
        refresh: function () {
            clearTimeout(debounceTimer);
            runSearch();
        }
    };

    document.addEventListener('DOMContentLoaded', init);
})();
