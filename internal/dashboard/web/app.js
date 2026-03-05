(function () {
    var token = localStorage.getItem('foreman_token') || prompt('Enter auth token:');
    if (token) localStorage.setItem('foreman_token', token);

    var headers = { 'Authorization': 'Bearer ' + token };
    var wsConnected = false;
    var daemonState = 'stopped';

    function fetchJSON(url) {
        return fetch(url, { headers: headers }).then(function (r) { return r.json(); });
    }

    /* ── Status dot ── */
    function updateDot() {
        var dot = document.getElementById('status-dot');
        var txt = document.getElementById('status-text');
        dot.className = 'status-dot';
        if (!wsConnected) {
            dot.classList.add('disconnected');
            txt.textContent = 'DISCONNECTED';
        } else if (daemonState === 'paused') {
            dot.classList.add('paused');
            txt.textContent = 'PAUSED';
        } else if (daemonState === 'running') {
            dot.classList.add('running');
            txt.textContent = 'RUNNING';
        } else {
            dot.classList.add('paused');
            txt.textContent = 'STOPPED';
        }
    }

    /* ── API polls ── */
    function loadStatus() {
        fetchJSON('/api/status').then(function (data) {
            daemonState = data.daemon_state || 'stopped';
            updateDot();
        }).catch(function () {
            daemonState = 'stopped';
            updateDot();
        });
    }

    function loadCosts() {
        Promise.all([
            fetchJSON('/api/costs/today'),
            fetchJSON('/api/costs/budgets')
        ]).then(function (results) {
            var today = results[0];
            var budget = results[1];
            var cost = today.cost_usd || 0;
            var el = document.getElementById('cost-display');
            if (budget.max_daily_usd) {
                var pct = (cost / budget.max_daily_usd) * 100;
                var threshold = budget.alert_threshold_pct || 80;
                el.textContent = 'COST: $' + cost.toFixed(2) + ' / $' + Math.round(budget.max_daily_usd);
                el.className = pct >= threshold ? 'over-budget' : '';
            } else {
                el.textContent = 'COST: $' + cost.toFixed(2);
                el.className = '';
            }
        }).catch(function () {});
    }

    function loadActive() {
        fetchJSON('/api/pipeline/active').then(function (tickets) {
            document.getElementById('active-display').textContent =
                'ACTIVE: ' + (Array.isArray(tickets) ? tickets.length : 0);
        }).catch(function () {});
    }

    function loadTickets() {
        fetchJSON('/api/tickets').then(function (tickets) {
            document.getElementById('ticket-count').textContent = tickets.length;
            document.getElementById('tickets').innerHTML = tickets.map(function (t) {
                var status = (t.Status || 'unknown').toLowerCase();
                return '<div class="ticket">' +
                    '<div class="ticket-title">' + (t.Title || t.ID) + '</div>' +
                    '<span class="ticket-status status-' + status + '">' +
                    status.toUpperCase() + '</span>' +
                    '</div>';
            }).join('');
        }).catch(function () {});
    }

    /* ── WebSocket ── */
    function connectWS() {
        var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        var ws = new WebSocket(proto + '//' + location.host + '/ws/events');
        var log = document.getElementById('event-log');

        ws.onopen = function () {
            wsConnected = true;
            updateDot();
        };

        ws.onmessage = function (e) {
            var evt = JSON.parse(e.data);
            var entry = document.createElement('div');
            entry.className = 'event-entry';
            entry.innerHTML =
                '<span class="event-time">' + new Date().toLocaleTimeString() + '</span>' +
                ' <span class="event-type">' + (evt.event_type || '') + '</span>' +
                '<span class="event-ticket">[' + (evt.ticket_id || '') + ']</span>';
            log.insertBefore(entry, log.firstChild);
            while (log.children.length > 200) { log.removeChild(log.lastChild); }
        };

        ws.onclose = function () {
            wsConnected = false;
            updateDot();
            setTimeout(connectWS, 3000);
        };
    }

    /* ── Boot ── */
    loadStatus();
    loadTickets();
    loadCosts();
    loadActive();

    setInterval(loadStatus,  15000);
    setInterval(loadTickets, 10000);
    setInterval(loadCosts,   60000);
    setInterval(loadActive,  30000);

    connectWS();
}());
