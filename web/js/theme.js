// Theme system.
//
// A theme is a directory under web/themes/<name>/:
//   theme.css  (required)  loaded ONLY while the theme is active, so it may
//               override anything: variables, layout, shapes, components.
//   theme.js   (optional)  injected after the app has fully loaded; may add
//               behavior (widgets, a music player, ...). Requires
//               "js": true in themes.json.
//
// Themes are listed in web/themes/themes.json (pure data, no code):
//   { "themes": [ { "name": "light", "label": "Light", "js": false } ] }
//
// The active theme name is persisted in localStorage. Switching themes
// reloads the page, because an injected theme script cannot be cleanly
// unloaded any other way.
//
// Note: web/ is embedded into the Go binary (main.go: //go:embed all:web),
// so themes ship with the app and adding one requires a rebuild.
(function () {
    var STORAGE_KEY = 'theme';
    var BASE = '/web/themes/';

    function getTheme() {
        return localStorage.getItem(STORAGE_KEY) || 'default';
    }

    function themeUrl(name, file) {
        return BASE + encodeURIComponent(name) + '/' + file;
    }

    var current = getTheme();

    // Marker on <html>, e.g. for theme scripts or small app-side tweaks.
    document.documentElement.setAttribute('data-theme', current);

    // In <head>, before first paint: pull in the active theme's CSS by
    // convention. A stale/unknown name just 404s and the :root defaults
    // apply.
    if (current !== 'default') {
        var link = document.createElement('link');
        link.rel = 'stylesheet';
        link.href = themeUrl(current, 'theme.css');
        document.head.appendChild(link);
    }

    function fetchManifest() {
        return fetch(BASE + 'themes.json').then(function (r) {
            return r.ok ? r.json() : { themes: [] };
        }).catch(function () {
            return { themes: [] };
        });
    }

    function findTheme(manifest, name) {
        return (manifest.themes || []).find(function (t) { return t.name === name; }) || null;
    }

    // After the app is fully loaded: inject the theme's script, if any.
    // Contract: theme scripts run on window "load", with the app ready.
    function injectThemeScript() {
        if (current === 'default') return;
        fetchManifest().then(function (manifest) {
            var t = findTheme(manifest, current);
            if (!t || !t.js) return;
            var s = document.createElement('script');
            s.src = themeUrl(current, 'theme.js');
            document.body.appendChild(s);
        });
    }

    function initThemeSelector() {
        var sel = document.getElementById('pref-theme');
        if (!sel) return;
        fetchManifest().then(function (manifest) {
            sel.innerHTML = '';
            [{ name: 'default', label: 'Default' }].concat(manifest.themes || [])
                .forEach(function (t) {
                    var opt = document.createElement('option');
                    opt.value = t.name;
                    opt.textContent = t.label || t.name;
                    sel.appendChild(opt);
                });
            if (current !== 'default' && !findTheme(manifest, current)) {
                // Saved theme no longer exists -> reset to default.
                current = 'default';
                localStorage.removeItem(STORAGE_KEY);
                document.documentElement.setAttribute('data-theme', current);
            }
            sel.value = current;
            sel.addEventListener('change', function () {
                var name = sel.value;
                if (name === 'default') {
                    localStorage.removeItem(STORAGE_KEY);
                } else {
                    localStorage.setItem(STORAGE_KEY, name);
                }
                // Theme CSS/JS are injected at page load only.
                location.reload();
            });
        });
    }

    document.addEventListener('DOMContentLoaded', initThemeSelector);
    window.addEventListener('load', injectThemeScript);

    window.DsTheme = { get: getTheme };
})();
