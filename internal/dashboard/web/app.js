(function() {
    const token = localStorage.getItem('foreman_token') || prompt('Enter auth token:');
    if (token) localStorage.setItem('foreman_token', token);

    const headers = { 'Authorization': 'Bearer ' + token };

    async function fetchJSON(url) {
        const res = await fetch(url, { headers });
        return res.json();
    }

    async function loadStatus() {
        const data = await fetchJSON('/api/status');
        document.getElementById('status').textContent = data.status + ' · ' + data.uptime;
    }

    async function loadTickets() {
        const tickets = await fetchJSON('/api/tickets');
        const el = document.getElementById('tickets');
        el.innerHTML = '<h2>Tickets (' + tickets.length + ')</h2>' +
            tickets.map(t => '<div class="ticket"><div class="title">' + t.Title +
                '</div><div class="status">' + t.Status + '</div></div>').join('');
    }

    function connectWS() {
        const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
        const ws = new WebSocket(proto + '//' + location.host + '/ws/events');
        const log = document.getElementById('event-log');

        ws.onmessage = function(e) {
            const evt = JSON.parse(e.data);
            const li = document.createElement('li');
            li.textContent = new Date().toLocaleTimeString() + ' ' + evt.event_type + ' [' + evt.ticket_id + ']';
            log.prepend(li);
            while (log.children.length > 200) log.removeChild(log.lastChild);
        };

        ws.onclose = function() {
            document.getElementById('status').textContent = 'disconnected';
            setTimeout(connectWS, 3000);
        };
    }

    loadStatus();
    loadTickets();
    setInterval(loadTickets, 10000);
    connectWS();
})();
