// validate.js - validation button
(function () {
    'use strict';
    document.getElementById('btn-validate').addEventListener('click', async function () {
        const btn = this;
        const chatTitle = ChatList.getCurrentTitle ? ChatList.getCurrentTitle() : null;
        if (!chatTitle) {
            btn.classList.add('error');
            setTimeout(function () {
                btn.classList.remove('error');
            }, 5000);
            return;
        }
        try {
            const resp = await fetch('/api/validate/' + encodeURIComponent(chatTitle));
            const data = await resp.json();
            if (data.errors && data.errors.length > 0) {
                Messages.highlightErrors(data.errors);
                btn.classList.add('error');
                setTimeout(function () {
                    btn.classList.remove('error');
                }, 5000);
            } else {
                btn.classList.add('success');
                setTimeout(function () {
                    btn.classList.remove('success');
                }, 5000);
                document.querySelectorAll('.highlight-error').forEach(function (el) {
                    el.classList.remove('highlight-error');
                });
            }
        } catch (e) {
            btn.classList.add('error');
            setTimeout(function () {
                btn.classList.remove('error');
            }, 5000);
        }
    });
})();
