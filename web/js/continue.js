// continue.js - Continue button + SSE consumption + interrupt
(function () {
    'use strict';
    var isRunning = false;
    var currentAssistant = null;
    var abortController = null;
    var streamingTitle = null;
    var messagesBeforeStream = 0;

    var btnContinue = document.getElementById('btn-continue');

    function setRunning(running) {
        isRunning = running;
        if (running) {
            btnContinue.textContent = 'Interrupt';
            btnContinue.classList.add('interrupt');
            btnContinue.disabled = false;
            if (ChatList.getCurrentTitle()) {
                localStorage.setItem('streaming_chat', ChatList.getCurrentTitle());
            }
        } else {
            btnContinue.textContent = 'Send';
            btnContinue.classList.remove('interrupt');
            btnContinue.disabled = false;
            localStorage.removeItem('streaming_chat');
        }
    }

    btnContinue.addEventListener('click', function () {
        if (isRunning) {
            doInterrupt();
        } else {
            doContinue();
        }
    });

    document.getElementById('user-input').addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            if (isRunning) {
                doInterrupt();
            } else {
                doContinue();
            }
        }
    });

    async function doInterrupt() {
        try {
            await fetch('/api/chat/interrupt', {method: 'POST'});
        } catch (e) {
        }
        if (abortController) {
            abortController.abort();
            abortController = null;
        }
        currentAssistant = null;
        streamingTitle = null;
        setRunning(false);
    }

    async function doContinue() {
        var title = ChatList.getCurrentTitle();
        if (!title) return;

        var inputArea = document.getElementById('user-input');
        var input = inputArea.value.trim();
        var autoContinue = document.getElementById('auto-continue').checked;

        setRunning(true);
        abortController = new AbortController();
        inputArea.value = '';
        currentAssistant = null;
        streamingTitle = title;

        // Remember how many messages are already rendered
        var container = document.getElementById('messages');
        // Remove any leftover streaming assistant (from auto-resume reconnect)
        var streamingEl = container.querySelector('.assistant-streaming');
        if (streamingEl) {
            streamingEl.remove();
        }
        messagesBeforeStream = container.querySelectorAll('.message').length;

        try {
            var resp = await fetch('/api/chat/continue', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    title: title,
                    input: input || '',
                    auto_continue: autoContinue
                }),
                signal: abortController.signal
            });

            if (!resp.ok) {
                var err = await resp.json();
                alert('Error: ' + (err.error || 'Unknown'));
                setRunning(false);
                return;
            }

            var reader = resp.body.getReader();
            var decoder = new TextDecoder();
            var buffer = '';

            try {
                while (true) {
                    var _a = await reader.read(), done = _a.done, value = _a.value;
                    if (done) break;

                    buffer += decoder.decode(value, {stream: true});
                    var lines = buffer.split('\n');
                    buffer = lines.pop() || '';

                    var eventType = '';
                    var eventData = '';

                    for (var i = 0; i < lines.length; i++) {
                        var line = lines[i];
                        if (line.startsWith('event: ')) {
                            eventType = line.substring(7).trim();
                        } else if (line.startsWith('data: ')) {
                            eventData = line.substring(6).trim();
                        } else if (line === '') {
                            if (eventType && eventData) {
                                handleEvent(eventType, eventData);
                            }
                            eventType = '';
                            eventData = '';
                        }
                    }
                }
            } catch (e) {
                if (e.name === 'AbortError') {
                    return;
                }
                console.error('Stream error:', e);
            }
        } catch (e) {
            if (e.name === 'AbortError') {
                return;
            }
            console.error('Continue error:', e);
        } finally {
            setRunning(false);
            abortController = null;
            await ChatList.loadMessages();
        }
    }

    function handleEvent(type, data) {
        // Guard: ignore events from a different chat (user switched chats mid-stream)
        if (streamingTitle && ChatList.getCurrentTitle() !== streamingTitle) {
            return;
        }

        var evt;
        try {
            evt = JSON.parse(data);
        } catch (e) {
            return;
        }

        switch (type) {
            case 'delta':
                appendToAssistant(evt.content || evt.Content, 'content');
                break;
            case 'reasoning_delta':
                appendToAssistant(evt.content || evt.Content, 'reasoning');
                break;
            case 'tool_call':
                appendToolCall(evt.tool_call || {id: evt.ID, function: {name: evt.Name, arguments: evt.Args}});
                break;
            case 'tool_result':
                appendToolResult(evt.tool_result ? evt.tool_result.message : evt);
                break;
            case 'assistant_done':
                break;
            case 'tool_execute':
                break;
            case 'user_added':
                appendUserMessage(evt.content || evt.Content || '');
                break;
            case 'error':
                if (evt.error && evt.error.ids) {
                    evt.error.ids.forEach(function (id) {
                        var el = document.querySelector('.tool-call-item[data-tool-call-id="' + id + '"]');
                        if (el) {
                            el.classList.add('highlight-error');
                            setTimeout(function () {
                                el.classList.remove('highlight-error');
                            }, 5000);
                        }
                    });
                }
                if (evt.error) {
                    console.warn('Continue error:', evt.error.detail || evt.error.type);
                }
                break;
        }
    }

    function getOrCreateAssistant() {
        var container = document.getElementById('messages');

        if (currentAssistant && currentAssistant.parentNode === container) {
            return currentAssistant;
        }

        // Always create a fresh assistant - never reuse stale DOM
        if (currentAssistant) {
            currentAssistant.remove();
        }
        currentAssistant = document.createElement('div');
        currentAssistant.className = 'message role-assistant assistant-streaming';
        container.appendChild(currentAssistant);
        container.scrollTop = container.scrollHeight;
        return currentAssistant;
    }

    function appendToAssistant(text, field) {
        var el = getOrCreateAssistant();
        if (field === 'reasoning') {
            var reasoningEl = el.querySelector('.reasoning-block');
            if (!reasoningEl) {
                reasoningEl = document.createElement('div');
                reasoningEl.className = 'reasoning-block';
                var toggle = document.createElement('div');
                toggle.className = 'reasoning-toggle';
                toggle.textContent = 'Reasoning \u25BC';
                var contentEl = document.createElement('div');
                contentEl.style.display = 'block';
                contentEl.dataset.rawText = '';
                toggle.addEventListener('click', function () {
                    if (contentEl.style.display === 'none') {
                        contentEl.style.display = 'block';
                        toggle.textContent = 'Reasoning \u25BC';
                    } else {
                        contentEl.style.display = 'none';
                        toggle.textContent = 'Reasoning \u25B6';
                    }
                });
                reasoningEl.appendChild(toggle);
                reasoningEl.appendChild(contentEl);
                el.appendChild(reasoningEl);
            }
            var contentEl = reasoningEl.querySelector('div');
            contentEl.dataset.rawText = (contentEl.dataset.rawText || '') + text;
            contentEl.innerHTML = marked.parse(contentEl.dataset.rawText);
        } else {
            var contentEl = el.querySelector('.msg-content');
            if (!contentEl) {
                contentEl = document.createElement('div');
                contentEl.className = 'msg-content';
                contentEl.dataset.rawText = '';
                el.appendChild(contentEl);
            }
            contentEl.dataset.rawText = (contentEl.dataset.rawText || '') + text;
            contentEl.innerHTML = marked.parse(contentEl.dataset.rawText);
        }
        document.getElementById('messages').scrollTop = document.getElementById('messages').scrollHeight;
    }

    function appendToolCall(tc) {
        var name = tc.function ? tc.function.name : (tc.Name || tc.name || '');
        var args = tc.function ? tc.function.arguments : (tc.Args || tc.arguments || '{}');
        var id = tc.id || tc.ID || '';

        var el = getOrCreateAssistant();
        var tcl = el.querySelector('.tool-calls-list');
        if (!tcl) {
            tcl = document.createElement('div');
            tcl.className = 'tool-calls-list';
            el.appendChild(tcl);
        }

        var existing = tcl.querySelector('.tool-call-item[data-tool-call-id="' + id + '"]');
        var item;
        if (existing) {
            item = existing;
        } else {
            item = document.createElement('div');
            item.className = 'tool-call-item';
            item.dataset.toolCallId = id;
            tcl.appendChild(item);
        }

        try {
            var formatted = JSON.stringify(JSON.parse(args), null, 2);
        } catch (e) {
            formatted = args;
        }
        item.innerHTML = '<div class="tool-call-name">' + Messages.escHtml(name) + '</div>'
            + '<div class="tool-call-args">' + Messages.escHtml(formatted) + '</div>';

        document.getElementById('messages').scrollTop = document.getElementById('messages').scrollHeight;
    }

    function appendToolResult(msg) {
        var container = document.getElementById('messages');
        var div = document.createElement('div');
        div.className = 'message role-tool';
        div.innerHTML = '<div class="msg-header"><span class="msg-role">TOOL</span><span class="msg-tags"><span class="msg-tag">' + Messages.escHtml(msg.name || '') + '</span></span></div>'
            + '<div class="msg-content"><pre><code>' + Messages.escHtml(msg.content || '') + '</code></pre></div>';
        container.appendChild(div);

        currentAssistant = null;
        document.getElementById('messages').scrollTop = document.getElementById('messages').scrollHeight;
    }

    function appendUserMessage(content) {
        var container = document.getElementById('messages');

        // Skip if this user message is already rendered (reconnect after refresh)
        var existing = container.querySelectorAll('.message.role-user');
        var msgs = container.querySelectorAll('.message');
        var currentCount = msgs.length;
        var renderedCount = currentCount - container.querySelectorAll('.assistant-streaming').length;
        // If this is a reconnect (no user input), the user message from the first event
        // may already be rendered. Check by looking at messages added after stream start.
        // But we cleared the streaming assistant, so messagesBeforeStream doesn't include it.
        // If the number of existing user messages >= messagesBeforeStream + "already rendered",
        // skip creating a duplicate. Simpler: always create, let loadMessages at the end fix it.

        var emptyState = container.querySelector('.empty-state');
        if (emptyState) {
            emptyState.remove();
        }

        var div = document.createElement('div');
        div.className = 'message role-user';
        div.innerHTML = '<div class="msg-header"><span class="msg-role">USER</span></div>'
            + '<div class="msg-content">' + marked.parse(content) + '</div>';
        container.appendChild(div);
        container.scrollTop = container.scrollHeight;
    }

    function tryAutoResume() {
        var streamingTitle = localStorage.getItem('streaming_chat');
        if (streamingTitle && streamingTitle === ChatList.getCurrentTitle()) {
            setRunning(true);
            doContinue();
        }
    }

    window.ContinueModule = {
        tryAutoResume: tryAutoResume,
        isRunning: function () {
            return isRunning;
        },
        setRunning: setRunning,
        doInterrupt: doInterrupt
    };
})();
