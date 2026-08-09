// continue.js - reentrant stream subscription + send/interrupt
//
// Protocol (per chat):
//   GET /api/chat/stream?title=X  (SSE, EventSource)
//     event: sync   {gen, saved_pos, running} - on connect and on new run
//     event: <delta|reasoning_delta|tool_call|tool_result|user_added|...> - replay + live
//     event: idle   {gen} - run finished (or immediately when idle)
//   POST /api/chat/continue {title, input, auto_continue} - start a run
//   POST /api/chat/interrupt {title} - stop a run
//
// Rendering invariant: history DOM reflects disk (messages up to saved_pos),
// the streaming layer (".stream-live" elements) is rebuilt from the event
// log starting at saved_pos. Any reload of history re-baselines saved_pos
// and replays only events with seq >= saved_pos, so nothing is ever
// rendered twice and no message is ever split.
(function () {
    'use strict';

    var isRunning = false;
    var currentAssistant = null;
    var toastTimer = null;
    var stopSoundFlag = false;
    var lastSoundAt = 0;
    var stopSoundContext = null;
    var activeAskKey = null;

    // ---- subscription state ----

    var evtSource = null;
    var subscribedTitle = null;
    var curGen = -1;
    var nextSeq = 0;          // seq (index in session event log) of the next event
    var historySavedPos = 0;  // saved_pos that the rendered history reflects
    var historyReady = false; // history rendered and baselined
    var pending = [];         // events buffered while historyReady === false
    var applied = [];         // DOM-producing events of the current streaming layer

    var STREAM_EVENT_TYPES = [
        'delta', 'reasoning_delta', 'tool_call', 'tool_execute',
        'tool_result', 'user_added', 'assistant_done', 'error'
    ];

    function showToast(msg) {
        var toast = document.getElementById('error-toast');
        if (!toast) {
            toast = document.createElement('div');
            toast.id = 'error-toast';
            document.body.appendChild(toast);
        }
        if (toastTimer) {
            clearTimeout(toastTimer);
        }
        toast.textContent = msg;
        toast.classList.add('visible');
        toastTimer = setTimeout(function () {
            toast.classList.remove('visible');
        }, 2500);
    }

    function scrollIfNearBottom(container) {
        var threshold = 80;
        if (container.scrollHeight - container.scrollTop - container.clientHeight < threshold) {
            container.scrollTop = container.scrollHeight;
        }
    }

    var btnContinue = document.getElementById('btn-continue');

    function setRunning(running) {
        isRunning = running;
        var noobActive = window.NoobMode && window.NoobMode.isActive();
        if (running) {
            btnContinue.textContent = 'Interrupt';
            btnContinue.classList.add('interrupt');
            btnContinue.disabled = noobActive;
        } else {
            btnContinue.textContent = 'Send';
            btnContinue.classList.remove('interrupt');
            btnContinue.disabled = false;
        }
    }

    btnContinue.addEventListener('click', function () {
        if (isRunning && window.NoobMode && window.NoobMode.isActive()) return;
        if (isRunning) {
            doInterrupt();
        } else {
            doContinue();
        }
    });

    document.getElementById('user-input').addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            if (isRunning && window.NoobMode && window.NoobMode.isActive()) return;
            if (isRunning) {
                doInterrupt();
            } else {
                doContinue();
            }
        }
    });

    // ---- subscription ----

    function disconnect() {
        if (evtSource) {
            evtSource.close();
            evtSource = null;
        }
        subscribedTitle = null;
        historyReady = false;
        pending = [];
        applied = [];
        curGen = -1;
        setRunning(false);
    }

    function switchChat(title) {
        disconnect();
        if (!title) return;
        subscribedTitle = title;
        var es = new EventSource('/api/chat/stream?title=' + encodeURIComponent(title));
        evtSource = es;
        es.addEventListener('sync', function (e) {
            var d;
            try {
                d = JSON.parse(e.data);
            } catch (err) {
                return;
            }
            onSync(d);
        });
        STREAM_EVENT_TYPES.forEach(function (t) {
            es.addEventListener(t, function (e) {
                var evt;
                try {
                    evt = JSON.parse(e.data);
                } catch (err) {
                    return;
                }
                onStreamEvent(t, evt);
            });
        });
        es.addEventListener('idle', function () {
            onIdle();
        });
        // EventSource auto-reconnects on errors; the server resends "sync"
        // on every reconnect, which resets and replays the streaming layer.
    }

    function onSync(d) {
        curGen = d.gen;
        nextSeq = d.saved_pos || 0;
        pending = [];
        applied = [];
        historyReady = false;
        resetStreamDOM();
        setRunning(!!d.running);
        // re-baseline history; onHistoryLoaded will flush pending events
        ChatList.loadMessages();
    }

    function onStreamEvent(type, evt) {
        var seq = nextSeq++;
        if (!historyReady) {
            pending.push({seq: seq, type: type, evt: evt});
            return;
        }
        if (seq < historySavedPos) {
            return; // already persisted, rendered as part of history
        }
        applyEvent(type, evt, true, seq);
    }

    function onIdle() {
        setRunning(false);
        applied = [];
        pending = [];
        setStopSoundFlag();
        // disk is now authoritative; replace the streaming layer
        ChatList.loadMessages();
    }

    // Called by ChatList.loadMessages after history has been re-rendered.
    // saved_pos tells us exactly which stream events are already on disk.
    function onHistoryLoaded(savedPos) {
        historySavedPos = savedPos || 0;
        historyReady = true;
        var kept = [];
        pending.forEach(function (p) {
            if (p.seq >= historySavedPos) {
                kept.push({seqStart: p.seq, seqEnd: p.seq, type: p.type, evt: p.evt});
            }
        });
        pending = [];
        // drop events that have been persisted since (saves happen at
        // message boundaries, so an entry is either fully saved or not)
        applied = applied.filter(function (a) {
            return a.seqEnd >= historySavedPos;
        });
        applied = mergeEvents(applied.concat(kept));
        replayApplied();
    }

    // merge adjacent delta events of the same kind so replays stay cheap
    function mergeEvents(list) {
        var out = [];
        list.forEach(function (item) {
            var last = out[out.length - 1];
            if (last && last.type === item.type
                && (item.type === 'delta' || item.type === 'reasoning_delta')) {
                last.evt = {content: (last.evt.content || '') + (item.evt.content || '')};
                last.seqEnd = item.seqEnd;
                return;
            }
            if (item.type === 'delta' || item.type === 'reasoning_delta') {
                out.push({
                    seqStart: item.seqStart,
                    seqEnd: item.seqEnd,
                    type: item.type,
                    evt: {content: item.evt.content || ''}
                });
            } else {
                out.push(item);
            }
        });
        return out;
    }

    function replayApplied() {
        resetStreamDOM();
        applied.forEach(function (a) {
            applyEvent(a.type, a.evt, false, 0);
        });
    }

    function resetStreamDOM() {
        var container = document.getElementById('messages');
        container.querySelectorAll('.stream-live').forEach(function (el) {
            el.remove();
        });
        currentAssistant = null;
    }

    // assistant_done is recorded too: it produces no DOM, but acts as a
    // barrier so deltas of two different assistant messages never merge
    // across a save boundary
    function isDomEvent(type) {
        return type === 'delta' || type === 'reasoning_delta'
            || type === 'tool_call' || type === 'tool_result' || type === 'user_added'
            || type === 'assistant_done';
    }

    function applyEvent(type, evt, record, seq) {
        switch (type) {
            case 'delta':
                appendToAssistant(evt.content || evt.Content || '', 'content');
                break;
            case 'reasoning_delta':
                appendToAssistant(evt.content || evt.Content || '', 'reasoning');
                break;
            case 'tool_call':
                if (!(window.NoobMode && window.NoobMode.isActive())) {
                    appendToolCall(evt.tool_call || {id: evt.ID, function: {name: evt.Name, arguments: evt.Args}});
                }
                break;
            case 'tool_result':
                if (!(window.NoobMode && window.NoobMode.isActive())) {
                    appendToolResult(evt.tool_result ? evt.tool_result.message : evt);
                }
                break;
            case 'user_added':
                appendUserMessage(evt.content || evt.Content || '');
                break;
            case 'assistant_done':
                // next stream segment starts a new assistant message
                currentAssistant = null;
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
                    showToast(evt.error.detail || evt.error.type || 'Unknown error');
                }
                break;
        }
        if (record && isDomEvent(type)) {
            pushApplied(type, evt, seq);
        }
    }

    function pushApplied(type, evt, seq) {
        var last = applied[applied.length - 1];
        if (last && last.type === type && (type === 'delta' || type === 'reasoning_delta')) {
            last.evt.content = (last.evt.content || '') + (evt.content || evt.Content || '');
            last.seqEnd = seq;
            return;
        }
        if (type === 'delta' || type === 'reasoning_delta') {
            applied.push({seqStart: seq, seqEnd: seq, type: type, evt: {content: evt.content || evt.Content || ''}});
        } else {
            applied.push({seqStart: seq, seqEnd: seq, type: type, evt: evt});
        }
    }

    // ---- send / interrupt ----

    async function doInterrupt() {
        var title = ChatList.getCurrentTitle();
        try {
            await fetch('/api/chat/interrupt', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({title: title})
            });
        } catch (e) {
        }
        setRunning(false);
    }

    async function doContinue(forcedInput) {
        clearStopSoundFlag();
        var title = ChatList.getCurrentTitle();
        if (!title) {
            showToast('Please create or open a session');
            return;
        }

        if (await reopenPendingAsk(title)) {
            showToast('Please answer the pending question first');
            return;
        }

        var inputArea = document.getElementById('user-input');
        var input = forcedInput !== undefined ? forcedInput : inputArea.value.trim();
        var autoContinue = document.getElementById('auto-continue').checked;
        if (forcedInput === undefined) {
            inputArea.value = '';
        }

        try {
            var resp = await fetch('/api/chat/continue', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    title: title,
                    input: input || '',
                    auto_continue: autoContinue
                })
            });
            if (!resp.ok) {
                var err = await resp.json();
                showToast(err.error || 'Unknown');
                return;
            }
            // running state and rendering are driven by the SSE subscription
            setRunning(true);
        } catch (e) {
            showToast('Continue error: ' + (e.message || e));
        }
    }

    async function reopenPendingAsk(title) {
        try {
            var resp = await fetch('/api/chats/' + encodeURIComponent(title));
            if (!resp.ok) return false;
            var chat = await resp.json();
            var pendingAsk = getPendingAsk(chat);
            if (!pendingAsk) return false;
            showAskUserModal(pendingAsk);
            return true;
        } catch (e) {
            return false;
        }
    }

    // ---- streaming DOM builders (all elements tagged .stream-live) ----

    function getOrCreateAssistant() {
        var container = document.getElementById('messages');

        if (currentAssistant && currentAssistant.parentNode === container) {
            return currentAssistant;
        }

        if (currentAssistant) {
            currentAssistant.remove();
        }
        currentAssistant = document.createElement('div');
        currentAssistant.className = 'message role-assistant assistant-streaming stream-live';
        container.appendChild(currentAssistant);
        scrollIfNearBottom(container);
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
                toggle.textContent = 'Reasoning ▶';
                var contentEl = document.createElement('div');
                contentEl.className = 'reasoning-content';
                contentEl.style.display = 'none';
                contentEl.dataset.rawText = '';
                toggle.addEventListener('click', function () {
                    if (contentEl.style.display === 'none') {
                        contentEl.style.display = 'block';
                        toggle.textContent = 'Reasoning ▼';
                    } else {
                        contentEl.style.display = 'none';
                        toggle.textContent = 'Reasoning ▶';
                    }
                });
                reasoningEl.appendChild(toggle);
                reasoningEl.appendChild(contentEl);
                el.appendChild(reasoningEl);
            }
            var contentEl = reasoningEl.querySelector('.reasoning-content');
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
        scrollIfNearBottom(document.getElementById('messages'));
    }

    function appendToolCall(tc) {
        var name = tc.function ? tc.function.name : (tc.Name || tc.name || '');
        var args = tc.function ? tc.function.arguments : (tc.Args || tc.arguments || '{}');
        var id = tc.id || tc.ID || '';

        var el = getOrCreateAssistant();
        var tcBlock = el.querySelector('.tool-calls-block');
        if (!tcBlock) {
            tcBlock = document.createElement('div');
            tcBlock.className = 'tool-calls-block';
            var tcToggle = document.createElement('div');
            tcToggle.className = 'tool-calls-toggle';
            tcToggle.textContent = 'Tool Calls (1) ▶';
            var tcl = document.createElement('div');
            tcl.className = 'tool-calls-list';
            tcl.style.display = 'none';
            tcToggle.addEventListener('click', function () {
                var count = tcl.querySelectorAll('.tool-call-item').length;
                if (tcl.style.display === 'none') {
                    tcl.style.display = 'flex';
                    tcToggle.textContent = 'Tool Calls (' + count + ') ▼';
                } else {
                    tcl.style.display = 'none';
                    tcToggle.textContent = 'Tool Calls (' + count + ') ▶';
                }
            });
            tcBlock.appendChild(tcToggle);
            tcBlock.appendChild(tcl);
            el.appendChild(tcBlock);
        }

        var tcl = tcBlock.querySelector('.tool-calls-list');
        var tcToggle = tcBlock.querySelector('.tool-calls-toggle');

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

        var count = tcl.querySelectorAll('.tool-call-item').length;
        if (tcl.style.display === 'none') {
            tcToggle.textContent = 'Tool Calls (' + count + ') ▶';
        } else {
            tcToggle.textContent = 'Tool Calls (' + count + ') ▼';
        }

        scrollIfNearBottom(document.getElementById('messages'));
    }

    function appendToolResult(msg) {
        var container = document.getElementById('messages');
        var div = document.createElement('div');
        div.className = 'message role-tool stream-live';

        var headerDiv = document.createElement('div');
        headerDiv.className = 'msg-header';
        headerDiv.innerHTML = '<span class="msg-role">TOOL</span><span class="msg-tags"><span class="msg-tag">' + Messages.escHtml(msg.name || '') + '</span></span>';

        var contentDiv = document.createElement('div');
        contentDiv.className = 'msg-content';
        contentDiv.style.display = 'none';
        contentDiv.innerHTML = '<pre><code>' + Messages.escHtml(msg.content || '') + '</code></pre>';

        var toolToggle = document.createElement('span');
        toolToggle.className = 'tool-result-toggle';
        toolToggle.textContent = ' ▶';
        toolToggle.addEventListener('click', function (e) {
            e.stopPropagation();
            if (contentDiv.style.display === 'none') {
                contentDiv.style.display = 'block';
                toolToggle.textContent = ' ▼';
            } else {
                contentDiv.style.display = 'none';
                toolToggle.textContent = ' ▶';
            }
        });
        headerDiv.appendChild(toolToggle);

        div.appendChild(headerDiv);
        div.appendChild(contentDiv);
        container.appendChild(div);

        currentAssistant = null;
        scrollIfNearBottom(document.getElementById('messages'));
    }

    function appendUserMessage(content) {
        var container = document.getElementById('messages');

        var emptyState = container.querySelector('.empty-state');
        if (emptyState) {
            emptyState.remove();
        }

        var div = document.createElement('div');
        div.className = 'message role-user stream-live';
        div.innerHTML = '<div class="msg-header"><span class="msg-role">USER</span></div>'
            + '<div class="msg-content">' + marked.parse(content) + '</div>';
        container.appendChild(div);
        scrollIfNearBottom(container);
    }

    // ---- stop sound ----

    function setStopSoundFlag() {
        stopSoundFlag = true;
        lastSoundAt = 0;
        updateMuteButton();
        pollStopSound();
    }

    function clearStopSoundFlag() {
        stopSoundFlag = false;
        updateMuteButton();
    }

    function pollStopSound() {
        if (!stopSoundFlag) return;
        if (localStorage.getItem('stop_sound') !== 'true') {
            clearStopSoundFlag();
            return;
        }

        var now = Date.now();
        if (now - lastSoundAt < 1000) return;
        lastSoundAt = now;
        playBeep();

        if (localStorage.getItem('loop_stop_sound') !== 'true') {
            clearStopSoundFlag();
        }
    }

    function updateMuteButton() {
        var btn = document.getElementById('btn-mute-sound');
        if (!btn) return;
        btn.style.display = stopSoundFlag
        && localStorage.getItem('stop_sound') === 'true'
        && localStorage.getItem('loop_stop_sound') === 'true' ? '' : 'none';
    }

    function playBeep() {
        try {
            var AudioCtx = window.AudioContext || window.webkitAudioContext;
            if (!AudioCtx) return;
            if (!stopSoundContext) stopSoundContext = new AudioCtx();
            var ctx = stopSoundContext;
            if (ctx.state === 'suspended') ctx.resume();

            var osc = ctx.createOscillator();
            var gain = ctx.createGain();
            osc.type = 'sine';
            osc.frequency.setValueAtTime(880, ctx.currentTime);
            osc.frequency.setValueAtTime(660, ctx.currentTime + 0.12);
            gain.gain.setValueAtTime(0.001, ctx.currentTime);
            gain.gain.exponentialRampToValueAtTime(0.35, ctx.currentTime + 0.02);
            gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.38);
            osc.connect(gain);
            gain.connect(ctx.destination);
            osc.start();
            osc.stop(ctx.currentTime + 0.4);
        } catch (e) {
        }
    }

    // ---- ask_user ----

    function extractAskQuestion(tc) {
        var args = tc && tc.function ? tc.function.arguments : '{}';
        try {
            var parsed = JSON.parse(args || '{}');
            return parsed.question || parsed.prompt || 'The assistant needs more information.';
        } catch (e) {
            return 'The assistant needs more information.';
        }
    }

    function getPendingAsk(chat) {
        if (!chat || !chat.messages || chat.messages.length === 0) return null;
        var idx = chat.messages.length - 1;
        var answered = {};
        while (idx >= 0 && chat.messages[idx].role === 'tool') {
            answered[chat.messages[idx].tool_call_id] = true;
            idx--;
        }
        var assistant = chat.messages[idx];
        if (!assistant || assistant.role !== 'assistant' || !assistant.tool_calls || assistant.tool_calls.length === 0) return null;

        var questions = [];
        assistant.tool_calls.forEach(function (tc) {
            if (tc && tc.function && tc.function.name === 'ask_user' && tc.id && !answered[tc.id]) {
                questions.push({toolCall: tc, question: extractAskQuestion(tc)});
            }
        });
        if (questions.length === 0) return null;
        return {messageIndex: idx, questions: questions};
    }

    function maybeShowAskUser(chat) {
        if (isRunning) return;
        var pending = getPendingAsk(chat);
        if (!pending) {
            hideAskUserModal();
            return;
        }
        var key = pending.questions.map(function (q) {
            return q.toolCall.id;
        }).join('|');
        if (activeAskKey === key) return;
        showAskUserModal(pending);
    }

    function showAskUserModal(pending) {
        var overlay = document.getElementById('ask-user-overlay');
        var listEl = document.getElementById('ask-user-list');
        if (!overlay || !listEl) return;

        activeAskKey = pending.questions.map(function (q) {
            return q.toolCall.id;
        }).join('|');
        listEl.innerHTML = '';
        pending.questions.forEach(function (q, index) {
            var item = document.createElement('div');
            item.className = 'ask-user-item';
            item.dataset.toolCallId = q.toolCall.id;

            var question = document.createElement('div');
            question.className = 'ask-user-question';
            question.textContent = q.question;

            var textarea = document.createElement('textarea');
            textarea.className = 'ask-user-answer';
            textarea.placeholder = 'Type your answer...';
            textarea.rows = 4;

            var row = document.createElement('div');
            row.className = 'ask-user-item-actions';
            var status = document.createElement('span');
            status.className = 'ask-user-status';
            var saveBtn = document.createElement('button');
            saveBtn.type = 'button';
            saveBtn.textContent = 'Save';
            saveBtn.addEventListener('click', function () {
                saveAskAnswer(pending, q, textarea, saveBtn, status, item);
            });

            row.appendChild(status);
            row.appendChild(saveBtn);
            item.appendChild(question);
            item.appendChild(textarea);
            item.appendChild(row);
            listEl.appendChild(item);

            if (index === 0) {
                setTimeout(function () {
                    textarea.focus();
                }, 0);
            }
        });
        overlay.classList.remove('hidden');
    }

    async function saveAskAnswer(pending, q, textarea, saveBtn, status, item) {
        var answer = textarea.value.trim();
        if (!answer) return;
        saveBtn.disabled = true;
        saveBtn.textContent = 'Saving...';
        status.textContent = '';
        try {
            var title = ChatList.getCurrentTitle();
            var chatResp = await fetch('/api/chats/' + encodeURIComponent(title));
            if (!chatResp.ok) throw new Error('Failed to reload chat');
            var chat = await chatResp.json();
            var resp = await fetch('/api/chat/' + encodeURIComponent(title) + '/message/' + chat.messages.length, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    role: 'tool',
                    name: 'ask_user',
                    tool_call_id: q.toolCall.id,
                    content: answer,
                    send_to_server: true
                })
            });
            if (!resp.ok) {
                var data = await resp.json();
                throw new Error(data.error || 'Failed to save answer');
            }

            textarea.disabled = true;
            saveBtn.textContent = 'Saved';
            status.textContent = 'Saved';
            item.classList.add('saved');

            if (allAskAnswersSaved()) {
                closeAskUserModal();
                await ChatList.loadMessages();
                doContinue('');
            }
        } catch (e) {
            saveBtn.disabled = false;
            saveBtn.textContent = 'Save';
            status.textContent = e.message || 'Failed';
        }
    }

    function allAskAnswersSaved() {
        var items = document.querySelectorAll('#ask-user-list .ask-user-item');
        if (!items.length) return false;
        for (var i = 0; i < items.length; i++) {
            if (!items[i].classList.contains('saved')) return false;
        }
        return true;
    }

    function closeAskUserModal() {
        hideAskUserModal();
        clearStopSoundFlag();
    }

    function hideAskUserModal() {
        var overlay = document.getElementById('ask-user-overlay');
        if (overlay) overlay.classList.add('hidden');
        activeAskKey = null;
    }

    document.getElementById('ask-user-close').addEventListener('click', closeAskUserModal);
    document.getElementById('ask-user-cancel').addEventListener('click', closeAskUserModal);
    document.getElementById('ask-user-overlay').addEventListener('click', function (e) {
        if (e.target === this) closeAskUserModal();
    });
    document.addEventListener('keydown', clearStopSoundFlag);
    document.addEventListener('mousemove', clearStopSoundFlag);

    var muteBtn = document.getElementById('btn-mute-sound');
    if (muteBtn) {
        muteBtn.addEventListener('click', clearStopSoundFlag);
    }
    setInterval(pollStopSound, 1000);

    window.AskUserPrompt = {
        maybeShow: maybeShowAskUser,
        updateMuteButton: updateMuteButton
    };

    window.ContinueModule = {
        isRunning: function () {
            return isRunning;
        },
        doInterrupt: doInterrupt,
        switchChat: switchChat,
        disconnect: disconnect,
        onHistoryLoaded: onHistoryLoaded
    };
})();
