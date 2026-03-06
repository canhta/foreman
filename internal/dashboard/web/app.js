// Auth
var token = localStorage.getItem('foreman_token') || prompt('Enter auth token:');
if (token) localStorage.setItem('foreman_token', token);
var headers = { 'Authorization': 'Bearer ' + token };

function fetchJSON(url) {
    return fetch(url, { headers: headers }).then(function (r) {
        if (!r.ok) throw new Error(r.statusText);
        return r.json();
    });
}

function postJSON(url) {
    return fetch(url, { method: 'POST', headers: headers }).then(function (r) {
        if (!r.ok) throw new Error(r.statusText);
        return r.json();
    });
}

function formatSender(jid) {
    if (!jid) return '';
    return jid.replace(/@s\.whatsapp\.net$/, '');
}

function formatTime(ts) {
    if (!ts) return '';
    return new Date(ts).toLocaleTimeString();
}

function formatRelative(ts) {
    if (!ts) return '';
    var diff = Date.now() - new Date(ts).getTime();
    var mins = Math.floor(diff / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return mins + 'm ago';
    var hours = Math.floor(mins / 60);
    if (hours < 24) return hours + 'h ago';
    return Math.floor(hours / 24) + 'd ago';
}

function severityIcon(severity) {
    switch (severity) {
        case 'success': return '\u2713';
        case 'error': return '\u2717';
        case 'warning': return '\u2753';
        default: return '\u25CF';
    }
}

var ACTIVE_STATUSES = ['planning', 'plan_validating', 'implementing', 'reviewing',
    'pr_created', 'awaiting_merge', 'clarification_needed', 'decomposing'];
var DONE_STATUSES = ['done', 'merged'];
var FAIL_STATUSES = ['failed', 'blocked', 'partial'];
var STUCK_THRESHOLD_MS = 30 * 60 * 1000; // 30 minutes

// Main Alpine.js component
function foreman() {
    return {
        // State
        tickets: [],
        selectedTicket: null,
        ticketDetail: null,
        ticketTasks: [],
        ticketLlmCalls: [],
        ticketEvents: [],
        events: [],
        filter: 'all',
        search: '',
        feedCollapsed: localStorage.getItem('feed_collapsed') === 'true',
        expandedTasks: {},
        activePanel: 'tickets', // mobile panel: tickets | detail | feed

        // Header state
        daemonState: 'stopped',
        whatsapp: null,
        dailyCost: 0,
        dailyBudget: 0,
        monthlyCost: 0,
        monthlyBudget: 0,
        weeklyCost: 0,
        activeCount: 0,

        // Team summary state
        teamStats: [],
        weekDays: [],
        recentPRs: [],

        // WebSocket
        ws: null,
        wsConnected: false,

        get daemonDotClass() {
            if (!this.wsConnected) return 'disconnected';
            if (this.daemonState === 'paused') return 'paused';
            if (this.daemonState === 'running') return 'running';
            return 'paused';
        },

        get daemonLabel() {
            if (!this.wsConnected) return 'DISCONNECTED';
            return this.daemonState.toUpperCase();
        },

        get costLabel() {
            if (this.dailyBudget > 0) {
                return 'COST: $' + this.dailyCost.toFixed(2) + ' / $' + Math.round(this.dailyBudget);
            }
            return 'COST: $' + this.dailyCost.toFixed(2);
        },

        get costOverBudget() {
            if (!this.dailyBudget) return false;
            return (this.dailyCost / this.dailyBudget) * 100 >= 80;
        },

        get filteredTickets() {
            var self = this;
            var list = this.tickets;

            // Filter by tab
            if (this.filter === 'active') {
                list = list.filter(function (t) { return ACTIVE_STATUSES.indexOf(t.Status) !== -1; });
            } else if (this.filter === 'done') {
                list = list.filter(function (t) { return DONE_STATUSES.indexOf(t.Status) !== -1; });
            } else if (this.filter === 'fail') {
                list = list.filter(function (t) { return FAIL_STATUSES.indexOf(t.Status) !== -1; });
            }

            // Search by title and submitter
            if (this.search) {
                var q = this.search.toLowerCase();
                list = list.filter(function (t) {
                    return (t.Title && t.Title.toLowerCase().indexOf(q) !== -1) ||
                        (t.ChannelSenderID && t.ChannelSenderID.toLowerCase().indexOf(q) !== -1);
                });
            }

            // Sort: failed pinned to top, then by UpdatedAt desc
            list = list.slice().sort(function (a, b) {
                var aFail = self.isFailed(a) ? 0 : 1;
                var bFail = self.isFailed(b) ? 0 : 1;
                if (aFail !== bFail) return aFail - bFail;
                return new Date(b.UpdatedAt) - new Date(a.UpdatedAt);
            });

            return list;
        },

        get needsAttention() {
            var now = Date.now();
            return this.tickets.filter(function (t) {
                if (FAIL_STATUSES.indexOf(t.Status) !== -1) return true;
                if (t.Status === 'clarification_needed') return true;
                // Stuck: active but no event in 30+ min
                if (ACTIVE_STATUSES.indexOf(t.Status) !== -1 && t.UpdatedAt) {
                    var elapsed = now - new Date(t.UpdatedAt).getTime();
                    if (elapsed > STUCK_THRESHOLD_MS) return true;
                }
                return false;
            });
        },

        init: function () {
            this.loadStatus();
            this.loadTickets();
            this.loadCosts();
            this.loadActive();
            this.connectWS();

            var self = this;
            setInterval(function () { self.loadStatus(); }, 15000);
            setInterval(function () { self.loadTickets(); }, 10000);
            setInterval(function () { self.loadCosts(); }, 60000);
            setInterval(function () { self.loadActive(); }, 30000);

            // Watch feed collapse state
            this.$watch('feedCollapsed', function (val) {
                localStorage.setItem('feed_collapsed', val);
            });
        },

        // API calls
        loadStatus: function () {
            var self = this;
            fetchJSON('/api/status').then(function (data) {
                self.daemonState = data.daemon_state || 'stopped';
                if (data.channels && data.channels.whatsapp) {
                    self.whatsapp = data.channels.whatsapp.connected;
                }
            }).catch(function () {
                self.daemonState = 'stopped';
            });
        },

        loadTickets: function () {
            var self = this;
            fetchJSON('/api/ticket-summaries').then(function (data) {
                self.tickets = data || [];
            }).catch(function () {});
        },

        loadCosts: function () {
            var self = this;
            Promise.all([
                fetchJSON('/api/costs/today'),
                fetchJSON('/api/costs/budgets'),
                fetchJSON('/api/costs/month'),
                fetchJSON('/api/costs/week')
            ]).then(function (results) {
                self.dailyCost = results[0].cost_usd || 0;
                self.dailyBudget = results[1].max_daily_usd || 0;
                self.monthlyCost = results[2].cost_usd || 0;
                self.monthlyBudget = results[1].max_monthly_usd || 0;

                // Weekly total
                var week = results[3] || [];
                self.weeklyCost = week.reduce(function (sum, d) { return sum + (d.cost_usd || 0); }, 0);
                self.weekDays = week;
            }).catch(function () {});
        },

        loadActive: function () {
            var self = this;
            fetchJSON('/api/pipeline/active').then(function (data) {
                self.activeCount = Array.isArray(data) ? data.length : 0;
            }).catch(function () {});
        },

        // Ticket selection
        selectTicket: function (t) {
            this.selectedTicket = t;
            this.loadTicketDetail(t.ID);
            // On mobile, auto-navigate to detail panel
            if (window.innerWidth < 768) {
                this.activePanel = 'detail';
            }
        },

        selectTicketById: function (id) {
            var t = this.tickets.find(function (t) { return t.ID === id; });
            if (t) this.selectTicket(t);
        },

        loadTicketDetail: function (id) {
            if (!id) return;
            var self = this;
            Promise.all([
                fetchJSON('/api/tickets/' + id),
                fetchJSON('/api/tickets/' + id + '/tasks'),
                fetchJSON('/api/tickets/' + id + '/llm-calls'),
                fetchJSON('/api/tickets/' + id + '/events')
            ]).then(function (results) {
                self.ticketDetail = results[0];
                self.ticketTasks = results[1] || [];
                self.ticketLlmCalls = results[2] || [];
                self.ticketEvents = results[3] || [];
                self.expandedTasks = {};
            }).catch(function () {});
        },

        // Filters
        countByFilter: function (f) {
            if (f === 'active') return this.tickets.filter(function (t) { return ACTIVE_STATUSES.indexOf(t.Status) !== -1; }).length;
            if (f === 'done') return this.tickets.filter(function (t) { return DONE_STATUSES.indexOf(t.Status) !== -1; }).length;
            if (f === 'fail') return this.tickets.filter(function (t) { return FAIL_STATUSES.indexOf(t.Status) !== -1; }).length;
            return this.tickets.length;
        },

        isFailed: function (t) {
            return FAIL_STATUSES.indexOf(t.Status) !== -1;
        },

        // Task helpers
        taskIcon: function (status) {
            switch (status) {
                case 'done': return '\u2713';
                case 'failed': return '\u2717';
                case 'implementing': case 'tdd_verifying': case 'testing':
                case 'spec_review': case 'quality_review': return '\u2699';
                case 'skipped': return '\u2298';
                default: return '\u25CB';
            }
        },

        taskIconClass: function (status) {
            if (status === 'done') return 'done';
            if (status === 'failed') return 'failed';
            if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].indexOf(status) !== -1) return 'active';
            return '';
        },

        toggleTask: function (taskId) {
            if (this.expandedTasks[taskId]) {
                delete this.expandedTasks[taskId];
            } else {
                this.expandedTasks[taskId] = true;
            }
            // Force reactivity
            this.expandedTasks = Object.assign({}, this.expandedTasks);
        },

        // Cost breakdown
        costByRole: function () {
            var roles = {};
            var total = 0;
            (this.ticketLlmCalls || []).forEach(function (c) {
                if (!roles[c.Role]) roles[c.Role] = 0;
                roles[c.Role] += c.CostUSD || 0;
                total += c.CostUSD || 0;
            });
            var result = [];
            for (var role in roles) {
                result.push({ role: role, cost: roles[role], pct: total > 0 ? (roles[role] / total * 100) : 0 });
            }
            result.sort(function (a, b) { return b.cost - a.cost; });
            return result;
        },

        llmSummary: function () {
            var calls = this.ticketLlmCalls || [];
            var totalTokens = 0;
            var models = {};
            var ok = 0;
            var retried = 0;
            calls.forEach(function (c) {
                totalTokens += (c.TokensInput || 0) + (c.TokensOutput || 0);
                models[c.Model] = true;
                if (c.Status === 'success') ok++;
                else retried++;
            });
            return {
                totalCalls: calls.length,
                ok: ok,
                retried: retried,
                totalTokens: totalTokens,
                model: Object.keys(models).join(', ') || '--'
            };
        },

        // Actions
        pauseDaemon: function () {
            if (!confirm('Pause the daemon?')) return;
            postJSON('/api/daemon/pause').catch(function (e) { alert('Failed: ' + e.message); });
        },

        resumeDaemon: function () {
            if (!confirm('Resume the daemon?')) return;
            postJSON('/api/daemon/resume').catch(function (e) { alert('Failed: ' + e.message); });
        },

        retryTicket: function (id) {
            if (!confirm('Retry this ticket?')) return;
            var self = this;
            postJSON('/api/tickets/' + id + '/retry').then(function () {
                self.loadTicketDetail(id);
                self.loadTickets();
            }).catch(function (e) { alert('Failed: ' + e.message); });
        },

        deleteTicket: function (id) {
            if (!confirm('Permanently delete this ticket and all its data?')) return;
            var self = this;
            fetch('/api/tickets/' + id, { method: 'DELETE', headers: headers })
                .then(function (r) {
                    if (!r.ok) throw new Error(r.statusText);
                    self.selectedTicket = null;
                    self.activePanel = 'tickets';
                    self.loadTickets();
                })
                .catch(function (e) { alert('Delete failed: ' + e.message); });
        },

        retryTask: function (taskId) {
            if (!confirm('Retry this task?')) return;
            var self = this;
            postJSON('/api/tasks/' + taskId + '/retry').then(function () {
                if (self.selectedTicket) self.loadTicketDetail(self.selectedTicket.ID);
            }).catch(function (e) { alert('Failed: ' + e.message); });
        },

        // WebSocket
        connectWS: function () {
            var self = this;
            var proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
            var ws = new WebSocket(proto + '//' + location.host + '/ws/events?token=' + encodeURIComponent(token));

            ws.onopen = function () {
                self.wsConnected = true;
            };

            ws.onmessage = function (e) {
                var evt = JSON.parse(e.data);
                evt.isNew = true;
                self.events.unshift(evt);
                if (self.events.length > 50) self.events.pop();

                // Clear isNew after animation
                setTimeout(function () { evt.isNew = false; }, 1200);
            };

            ws.onclose = function () {
                self.wsConnected = false;
                setTimeout(function () { self.connectWS(); }, 3000);
            };

            self.ws = ws;
        },

        formatSender: formatSender,
        formatTime: formatTime,
        formatRelative: formatRelative,
        severityIcon: severityIcon
    };
}

// Team summary sub-component
function teamSummary() {
    return {
        teamStats: [],
        weekDays: [],
        recentPRs: [],
        todayStats: { total: 0, merged: 0, failed: 0, active: 0 },

        load: function () {
            var self = this;

            Promise.all([
                fetchJSON('/api/stats/team'),
                fetchJSON('/api/stats/recent-prs')
            ]).then(function (results) {
                self.teamStats = results[0] || [];
                self.recentPRs = results[1] || [];
            }).catch(function () {});

            // Derive today stats from ticket list
            this.$nextTick(function () {
                self.computeTodayStats();
            });

            setInterval(function () {
                Promise.all([
                    fetchJSON('/api/stats/team'),
                    fetchJSON('/api/stats/recent-prs')
                ]).then(function (results) {
                    self.teamStats = results[0] || [];
                    self.recentPRs = results[1] || [];
                }).catch(function () {});
                self.computeTodayStats();
            }, 60000);
        },

        computeTodayStats: function () {
            // Access parent component data via Alpine
            var parent = Alpine.$data(this.$el.closest('[x-data]'));
            if (!parent || !parent.tickets) return;

            var today = new Date().toISOString().slice(0, 10);
            var todayTickets = parent.tickets.filter(function (t) {
                return t.CreatedAt && t.CreatedAt.slice(0, 10) === today;
            });
            this.todayStats = {
                total: todayTickets.length,
                merged: todayTickets.filter(function (t) { return DONE_STATUSES.indexOf(t.Status) !== -1; }).length,
                failed: todayTickets.filter(function (t) { return FAIL_STATUSES.indexOf(t.Status) !== -1; }).length,
                active: todayTickets.filter(function (t) { return ACTIVE_STATUSES.indexOf(t.Status) !== -1; }).length
            };
            this.weekDays = parent.weekDays || [];
        },

        maxWeekCost: function () {
            var max = 0;
            (this.weekDays || []).forEach(function (d) { if (d.cost_usd > max) max = d.cost_usd; });
            return max || 1;
        },

        barWidth: function (cost) {
            var max = this.maxWeekCost();
            var chars = Math.round((cost / max) * 16);
            var bar = '';
            for (var i = 0; i < chars; i++) bar += '\u2588';
            return bar;
        },

        dayLabel: function (dateStr) {
            var days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
            return days[new Date(dateStr).getDay()];
        },

        formatSender: formatSender,
        formatRelative: formatRelative
    };
}
