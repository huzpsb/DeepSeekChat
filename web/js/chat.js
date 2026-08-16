// chat.js - chat list management
var ChatList = {
    currentTitle: null,

    init: async function () {
        // sessionStorage so multiple tabs can each view their own chat;
        // fall back to the legacy localStorage key once
        var savedTitle = sessionStorage.getItem('current_chat') || localStorage.getItem('current_chat');
        await this.refresh();

        if (savedTitle) {
            this.currentTitle = savedTitle;
            var resp = await fetch('/api/chats/' + encodeURIComponent(savedTitle));
            if (resp.ok) {
                if (window.ContinueModule) {
                    window.ContinueModule.switchChat(savedTitle);
                }
                await this.loadMessages();
                document.querySelectorAll('#chat-list li .chat-title').forEach(function (span) {
                    if (span.textContent === savedTitle) {
                        span.closest('li').classList.add('active');
                    }
                });
            } else {
                // Chat was deleted — clean up stale state
                this.currentTitle = null;
                sessionStorage.removeItem('current_chat');
                localStorage.removeItem('current_chat');
            }
        }

        if (!this.currentTitle) {
            Messages.render(null, document.getElementById('messages'));
        }

        document.getElementById('btn-new-chat').addEventListener('click', function () {
            ChatList.create();
        });
    },

    getCurrentTitle: function () {
        return this.currentTitle;
    },

    saveCurrentTitle: function (title) {
        if (title) {
            sessionStorage.setItem('current_chat', title);
            localStorage.removeItem('current_chat');
        } else {
            sessionStorage.removeItem('current_chat');
        }
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
        // reconcile markers with the (possibly fresher) SSE status snapshot
        if (window.ChatStatus) {
            window.ChatStatus.applyMarkers();
        }
    },

    renderItem: function (chat) {
        var list = document.getElementById('chat-list');
        var li = document.createElement('li');
        li.dataset.title = chat.title;
        li.innerHTML = '<span class="chat-title">' + this.esc(chat.title) + '</span>'
            + (chat.running ? '<span class="chat-running" title="Generating...">●</span>' : '')
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
        var body = null;
        if (this.currentTitle) {
            try {
                var currentResp = await fetch('/api/chats/' + encodeURIComponent(this.currentTitle));
                if (currentResp.ok) {
                    var current = await currentResp.json();
                    if (current.root_dir) {
                        body = JSON.stringify({root_dir: current.root_dir});
                    }
                }
            } catch (e) {
                // If the current chat cannot be read, fall back to the
                // server default root dir rather than failing to create.
            }
        }
        var resp = await fetch('/api/chats', {
            method: 'POST',
            headers: body ? {'Content-Type': 'application/json'} : {},
            body: body
        });
        if (resp.ok) {
            var chat = await resp.json();
            this.currentTitle = chat.title;
            this.saveCurrentTitle(chat.title);
            if (window.ContinueModule) {
                window.ContinueModule.switchChat(chat.title);
            }
        }
        await this.refresh();
        await this.loadMessages();
    },

    select: async function (title) {
        // switch subscription only — a running chat keeps running in the background
        this.currentTitle = title;
        this.saveCurrentTitle(title);
        if (window.ContinueModule) {
            window.ContinueModule.switchChat(title);
        }
        document.querySelectorAll('#chat-list li').forEach(function (li) {
            li.classList.remove('active');
        });
        if (event && event.currentTarget) event.currentTarget.classList.add('active');
        await this.loadMessages();
    },

    updateContextSize: function (n) {
        var el = document.getElementById('context-size');
        if (!n) {
            el.textContent = '';
            el.style.display = 'none';
            return;
        }
        el.style.display = '';
        el.textContent = 'Ctx ' + this.formatTokens(n);
        el.title = n + ' input tokens (last request)';
    },

    formatTokens: function (n) {
        if (n >= 1000) {
            return (n / 1000).toFixed(1).replace(/\.0$/, '') + 'k';
        }
        return String(n);
    },

    loadMessages: async function () {
        if (!this.currentTitle) {
            Messages.render(null, document.getElementById('messages'));
            this.updateContextSize(0);
            if (window.DsApp && window.DsApp.updateRootDirSelector) {
                window.DsApp.updateRootDirSelector('');
            }
            if (window.ContinueModule) {
                window.ContinueModule.onHistoryLoaded(0);
            }
            return;
        }
        var resp = await fetch('/api/chats/' + encodeURIComponent(this.currentTitle));
        if (!resp.ok) {
            if (resp.status === 404) {
                this.currentTitle = null;
                this.saveCurrentTitle(null);
                Messages.render(null, document.getElementById('messages'));
                this.updateContextSize(0);
                if (window.ContinueModule) {
                    window.ContinueModule.onHistoryLoaded(0);
                }
            }
            return;
        }
        var chat = await resp.json();
        Messages.render(chat.messages || [], document.getElementById('messages'));
        this.updateContextSize(chat.context_size);
        if (window.DsApp && window.DsApp.updateRootDirSelector) {
            window.DsApp.updateRootDirSelector(chat.root_dir || '');
        }
        if (window.ContinueModule) {
            window.ContinueModule.onHistoryLoaded(chat.saved_pos || 0);
        }
        if (window.AskUserPrompt) {
            window.AskUserPrompt.maybeShow(chat);
        }
    },

    dupe: async function (title) {
        await fetch('/api/chats/' + encodeURIComponent(title) + '/dupe', {method: 'POST'});
        await this.refresh();
    },

    del: async function (title) {
        var resp = await fetch('/api/chats/' + encodeURIComponent(title), {method: 'DELETE'});
        if (!resp.ok) {
            var data = await resp.json();
            alert('Delete failed: ' + (data.error || 'Unknown'));
            return;
        }
        if (this.currentTitle === title) {
            this.currentTitle = null;
            this.saveCurrentTitle(null);
            if (window.ContinueModule) {
                window.ContinueModule.disconnect();
            }
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
                        self.saveCurrentTitle(newTitle);
                        if (window.ContinueModule) {
                            window.ContinueModule.switchChat(newTitle);
                        }
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
