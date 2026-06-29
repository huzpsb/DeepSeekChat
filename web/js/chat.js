// chat.js - chat list management
var ChatList = {
    currentTitle: null,

    init: async function () {
        var savedTitle = localStorage.getItem('current_chat');
        await this.refresh();

        if (savedTitle) {
            this.currentTitle = savedTitle;
            var resp = await fetch('/api/chats/' + encodeURIComponent(savedTitle));
            if (resp.ok) {
                await this.loadMessages();
                document.querySelectorAll('#chat-list li .chat-title').forEach(function (span) {
                    if (span.textContent === savedTitle) {
                        span.closest('li').classList.add('active');
                    }
                });
            } else {
                // Chat was deleted — clean up stale state
                this.currentTitle = null;
                localStorage.removeItem('current_chat');
            }
        }

        if (!this.currentTitle) {
            Messages.render(null, document.getElementById('messages'));
        }

        var streamingTitle = localStorage.getItem('streaming_chat');
        if (streamingTitle && streamingTitle !== this.currentTitle) {
            localStorage.removeItem('streaming_chat');
        }

        if (window.ContinueModule && this.currentTitle) {
            window.ContinueModule.tryAutoResume();
        }

        document.getElementById('btn-new-chat').addEventListener('click', function () {
            ChatList.create();
        });
    },

    getCurrentTitle: function () {
        return this.currentTitle;
    },

    refresh: async function () {
        var resp = await fetch('/api/chats');
        var chats = await resp.json();
        var list = document.getElementById('chat-list');
        list.innerHTML = '';
        if (chats && chats.length > 0) {
            chats.forEach(function (chat) {
                ChatList.renderItem(chat);
            });
        } else {
            list.innerHTML = '<li style="color:var(--text-secondary);padding:12px">No chats yet</li>';
        }
    },

    renderItem: function (chat) {
        var list = document.getElementById('chat-list');
        var li = document.createElement('li');
        li.innerHTML = '<span class="chat-title">' + this.esc(chat.title) + '</span>'
            + '<span class="chat-actions">'
            + '<button class="btn-rename" title="Rename">&#x270E;</button>'
            + '<button class="btn-dupe" title="Duplicate">&#x2398;</button>'
            + '<button class="btn-delete danger" title="Delete">&times;</button>'
            + '</span>';
        if (chat.title === this.currentTitle) {
            li.classList.add('active');
        }
        li.querySelector('.chat-title').addEventListener('dblclick', function () {
            ChatList.startRename(li, chat);
        });
        li.querySelector('.btn-rename').addEventListener('click', function (e) {
            e.stopPropagation();
            ChatList.startRename(li, chat);
        });
        li.querySelector('.btn-dupe').addEventListener('click', function (e) {
            e.stopPropagation();
            ChatList.dupe(chat.title);
        });
        li.querySelector('.btn-delete').addEventListener('click', function (e) {
            e.stopPropagation();
            ChatList.del(chat.title);
        });
        li.addEventListener('click', function () {
            ChatList.select(chat.title);
        });
        list.appendChild(li);
    },

    create: async function () {
        if (window.ContinueModule && window.ContinueModule.isRunning()) {
            window.ContinueModule.doInterrupt();
        }
        var resp = await fetch('/api/chats', {method: 'POST'});
        if (resp.ok) {
            var chat = await resp.json();
            this.currentTitle = chat.title;
            localStorage.setItem('current_chat', chat.title);
        }
        await this.refresh();
        await this.loadMessages();
    },

    select: async function (title) {
        // Stop any running stream before switching chats
        if (window.ContinueModule && window.ContinueModule.isRunning()) {
            window.ContinueModule.doInterrupt();
        }
        this.currentTitle = title;
        localStorage.setItem('current_chat', title);
        document.querySelectorAll('#chat-list li').forEach(function (li) {
            li.classList.remove('active');
        });
        if (event && event.currentTarget) event.currentTarget.classList.add('active');
        await this.loadMessages();
    },

    loadMessages: async function () {
        if (!this.currentTitle) {
            Messages.render(null, document.getElementById('messages'));
            return;
        }
        var resp = await fetch('/api/chats/' + encodeURIComponent(this.currentTitle));
        if (!resp.ok) {
            if (resp.status === 404) {
                this.currentTitle = null;
                localStorage.removeItem('current_chat');
                Messages.render(null, document.getElementById('messages'));
            }
            return;
        }
        var chat = await resp.json();
        Messages.render(chat.messages || [], document.getElementById('messages'));
        if (window.AskUserPrompt) {
            window.AskUserPrompt.maybeShow(chat);
        }
    },

    dupe: async function (title) {
        await fetch('/api/chats/' + encodeURIComponent(title) + '/dupe', {method: 'POST'});
        await this.refresh();
    },

    del: async function (title) {
        if (window.ContinueModule && window.ContinueModule.isRunning() && this.currentTitle === title) {
            window.ContinueModule.doInterrupt();
        }
        await fetch('/api/chats/' + encodeURIComponent(title), {method: 'DELETE'});
        if (this.currentTitle === title) {
            this.currentTitle = null;
            localStorage.removeItem('current_chat');
        }
        await this.refresh();
        await this.loadMessages();
    },

    startRename: function (li, chat) {
        var span = li.querySelector('.chat-title');
        var input = document.createElement('input');
        input.type = 'text';
        input.value = chat.title;
        input.style.cssText = 'flex:1;background:var(--bg-primary);color:var(--text-primary);border:1px solid var(--accent);padding:2px 4px;border-radius:2px;font-size:13px;';
        span.replaceWith(input);
        input.focus();
        input.select();
        var self = this;
        var save = async function () {
            var newTitle = input.value.trim();
            if (newTitle && newTitle !== chat.title) {
                var resp = await fetch('/api/chats/' + encodeURIComponent(chat.title) + '/rename', {
                    method: 'PUT',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({title: newTitle})
                });
                if (resp.ok) {
                    if (self.currentTitle === chat.title) {
                        self.currentTitle = newTitle;
                        localStorage.setItem('current_chat', newTitle);
                    }
                    input.removeEventListener('blur', save);
                    await self.refresh();
                    return;
                }
                var data = await resp.json();
                alert('Rename failed: ' + (data.error || 'Unknown'));
            }
            input.replaceWith(span);
        };
        var keyHandler = function (e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                input.removeEventListener('blur', save);
                save();
            }
            if (e.key === 'Escape') {
                input.removeEventListener('blur', save);
                input.replaceWith(span);
            }
        };
        input.addEventListener('blur', save);
        input.addEventListener('keydown', keyHandler);
    },

    esc: function (s) {
        var d = document.createElement('div');
        d.textContent = s;
        return d.innerHTML;
    }
};
