// sidebar.js - mobile drawer for the chat list sidebar
//
// Wide screens keep the old layout untouched: the sidebar is a
// permanent column, the header is a single line and the toggle button
// is display:none. Only once the sidebar would cover 1/3 or more of
// the viewport (viewport <= 780px == 260 * 3, matching the media query
// in style.css) does the compact layout kick in: the sidebar becomes a
// slide-in drawer and #btn-sidebar-toggle becomes visible. The drawer
// starts closed and is auto-closed after picking/creating a chat (see
// the Sidebar.autoClose calls in chat.js).
(function () {
    'use strict';

    // keep in sync with the media query in web/css/style.css
    var mql = window.matchMedia('(max-width: 780px)');

    var app = document.getElementById('app');
    var btn = document.getElementById('btn-sidebar-toggle');
    var backdrop = document.getElementById('sidebar-backdrop');

    function isCompact() {
        return mql.matches;
    }

    function isCollapsed() {
        return app.classList.contains('sidebar-collapsed');
    }

    function apply(collapsed) {
        app.classList.toggle('sidebar-collapsed', collapsed);
        btn.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
        btn.title = collapsed ? 'Show sidebar' : 'Hide sidebar';
    }

    // Wide: sidebar permanently expanded (old look and feel).
    // Compact: drawer closed, so phones land on the chat, not the list.
    // Done without animation (.preload) so neither the initial paint
    // nor a breakpoint crossing (rotation, window resize) slides the
    // drawer.
    function syncWithMode() {
        app.classList.add('preload');
        apply(isCompact());
        requestAnimationFrame(function () {
            requestAnimationFrame(function () {
                app.classList.remove('preload');
            });
        });
    }

    btn.addEventListener('click', function () {
        apply(!isCollapsed());
    });

    backdrop.addEventListener('click', function () {
        apply(true);
    });

    document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape' && isCompact() && !isCollapsed()) {
            apply(true);
        }
    });

    if (typeof mql.addEventListener === 'function') {
        mql.addEventListener('change', syncWithMode);
    } else if (typeof mql.addListener === 'function') {
        mql.addListener(syncWithMode); // old Safari/Edge
    }

    syncWithMode();

    window.Sidebar = {
        isCompact: isCompact,
        isCollapsed: isCollapsed,
        toggle: function () {
            apply(!isCollapsed());
        },
        // close the drawer after picking/creating a chat so the compact
        // layout lands on the conversation instead of the chat list
        autoClose: function () {
            if (isCompact() && !isCollapsed()) {
                apply(true);
            }
        }
    };
})();
