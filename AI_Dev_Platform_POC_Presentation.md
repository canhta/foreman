# AI-Assisted Development Platform — POC Presentation
### Board of Directors Review

---

## 1. Executive Summary

- We are proposing a **Proof of Concept (POC)** for an AI-Assisted Development Platform
- The platform enables **coding agents to operate continuously**, picking up and completing engineering tasks autonomously in the background
- This is not a replacement for engineers — it is a **force multiplier** that handles routine, well-defined tasks so engineers can focus on higher-value work
- The POC validates feasibility, safety, and integration before any broader rollout
- This concept is **partially inspired by "Antfarm"**, an approach where multiple coding agents work continuously on engineering tasks — however, we deliberately adopt a simpler, more cost-controlled architecture than existing implementations
- **Strategic goal:** reduce backlog growth, accelerate iteration speed, and build the foundation for autonomous engineering support — with **predictable cost and operational simplicity** as first-class design constraints

---

## 2. Why This System Exists — The Problem

### The Gap in Today's AI Coding Tools

- Tools like **Claude Code** and **GitHub Copilot** are powerful but **reactive** — they only work when a developer manually triggers them
- They cannot pick up tasks independently, run overnight, or process a backlog autonomously
- **Engineering backlogs continue to grow** — bug fixes, refactors, dependency upgrades, test coverage, and small improvements accumulate faster than teams can address them
- A large portion of backlog items are **low-to-medium complexity tasks** that follow predictable patterns — ideal candidates for automation

### The Missing Infrastructure

- There is currently **no safe, isolated environment** where AI-generated code changes can be automatically executed and tested before reaching production
- Running untested AI code directly against shared environments poses unacceptable risk
- **Sandbox environments** are needed to contain, test, and validate AI-generated changes safely

### The Opportunity

- If AI agents could run these tasks continuously, **24 hours a day, 7 days a week**, engineering throughput could increase significantly
- The bottleneck shifts from "time to implement" to "time to review and approve"

---

## 3. Vision of the System

### Continuous, Autonomous Engineering Assistance

- AI coding agents that **operate around the clock**, processing tasks without requiring manual initiation
- Agents automatically **pull tasks from the engineering backlog**, implement changes, run tests, and submit pull requests for human review
- Engineers **retain full control** — all AI-generated changes go through standard review and approval workflows
- **Sandbox environments** are provisioned automatically per task, ensuring full isolation and safety

### The Developer Experience Shift

| Today | With This Platform |
|---|---|
| Engineer manually picks up a backlog task | Agent picks up task automatically |
| Engineer implements, tests, and opens a PR | Agent implements, tests, and opens a PR |
| Engineer waits for CI to complete | Agent monitors CI and iterates if needed |
| Small tasks block larger work | Small tasks are handled in the background |

### Long-Term Vision

- A **self-sustaining engineering support layer** that continuously reduces backlog
- Agents that can handle an increasing share of routine engineering work as confidence in the system grows
- Engineers spending more time on **architecture, product decisions, and complex problem solving**

---

## 4. Lessons from Antfarm / OpenClaw

### The Inspiration

- This platform is **partially inspired by the "Antfarm" concept**, which runs on the **OpenClaw framework** — a system where multiple AI coding agents operate simultaneously in the background, continuously working through engineering tasks
- Antfarm demonstrated that **background multi-agent engineering is achievable** and showed real promise in processing backlogs without human initiation
- The core idea — agents pulling tasks, implementing code, and submitting PRs autonomously — is exactly the model we want to replicate

### What We Learned — Two Significant Problems

#### Problem 1: Token Cost

- The Antfarm / OpenClaw approach relies on **continuous agent loops** — agents that run indefinitely, polling for work, maintaining long context windows, and repeatedly re-processing state
- This leads to **extremely high token consumption** and operational cost that scales poorly with task volume
- At production scale, continuous loops become economically unviable without heavy subsidisation
- **Every unnecessary LLM call is a direct cost** — and continuous polling architectures generate many unnecessary calls

#### Problem 2: System Complexity

- OpenClaw is a **powerful but highly complex framework** — designed for maximum flexibility and breadth of use cases
- For an internal SaaS platform with specific, well-defined workflows, this complexity introduces significant **operational overhead**
- The framework requires deep expertise to maintain, debug, and extend — creating a knowledge dependency risk
- Complexity also makes it harder to reason about cost, behaviour, and failure modes, which is critical for a system handling production code changes

### What We Intentionally Simplify

| Antfarm / OpenClaw Approach | Our Simplified Approach |
|---|---|
| Continuous agent loops | **Event-driven execution** — agents run only when a task is assigned |
| General-purpose framework | **Purpose-built orchestration** for our specific workflow |
| Broad flexibility | **Constrained, predictable task types** |
| High token consumption | **Token budgets and execution limits per task** |
| Complex framework dependency | **Minimal, maintainable internal platform** |

### What We Preserve from the Antfarm Model

- The **core idea of background multi-agent operation** — tasks handled without manual developer initiation
- **Isolated workspaces per task** — preventing agents from interfering with each other
- **Autonomous PR creation** — agents complete the full implementation-to-PR cycle
- The principle that **AI agents should handle implementation; humans handle review**

---

## 5. Design Philosophy

### Four Guiding Principles

This platform is designed around four non-negotiable principles that distinguish it from more ambitious but riskier approaches:

#### 1. Simplicity Over Generality

- The platform solves a **specific, well-defined problem**: route tasks from a backlog to an AI agent, execute in isolation, and surface results for human review
- We resist building a general-purpose agent framework — every layer of abstraction adds complexity and potential failure points
- **The right tool for this job is the simplest tool that works reliably**

#### 2. Controllability

- Every agent execution is **bounded** — by time, by token budget, and by the scope of a single task
- The platform can **pause, stop, or roll back** any agent action without side effects on the broader codebase
- Human approval is a **required gate** before any AI-generated change enters the main codebase
- Agents operate with **least-privilege access** — they can only touch what they need for their assigned task

#### 3. Predictable Cost

- AI token consumption is **metered and capped per task** — no open-ended agent loops that can run indefinitely
- Infrastructure costs are controlled through **on-demand environment provisioning** and automatic teardown
- The platform maintains a **cost ledger** — every task has a measurable cost in tokens and compute, enabling informed scaling decisions
- We prefer **deterministic, non-LLM steps** wherever possible — routing logic, environment setup, test execution, and PR creation do not require AI inference

#### 4. Gradual Automation — Not Full Autonomy

- We do not aim for full autonomy in the initial phases — **human review is a feature, not a limitation**
- Autonomy increases **incrementally** as confidence in agent output quality is established through data
- Early phases focus on tasks with clear, measurable success criteria (tests pass, linter passes, PR is mergeable) before expanding to more complex work
- **Trust is earned through demonstrated reliability**, not assumed

### Why These Principles Matter for the BOD

- They ensure the platform **does not become a runaway cost centre** as usage scales
- They provide **clear guardrails** that engineering leadership can point to when explaining AI governance
- They make the system **auditable and explainable** — every decision the platform makes can be traced and justified
- They allow us to **start small and expand** without betting on unproven technology at full scale

---

## 6. System Interaction Flow

### High-Level Workflow

```
Jira Backlog → Orchestration Platform → AI Coding Agent → Sandbox Environment → GitHub PR → Notification
```

### Step-by-Step Flow

1. **Task Selection** — The platform monitors Jira for eligible tasks (labelled or categorised for AI handling)
2. **Agent Dispatch** — An AI coding agent (Claude Code) is assigned to the task and given access to the relevant code repository
3. **Isolated Workspace** — A dedicated sandbox environment and git workspace are provisioned for the task
4. **Implementation** — The agent reads the task, explores the codebase, implements the change, and runs automated tests
5. **Pull Request Creation** — Upon successful test completion, the agent opens a GitHub pull request with a clear description of the changes made
6. **Notification & Approval** — The assigned engineer receives a notification (e.g. via WhatsApp or Slack) with a link to review the PR
7. **Human Review** — The engineer reviews, requests changes if needed, and approves or merges the PR
8. **Environment Teardown** — The sandbox environment is decommissioned automatically after the PR is merged or closed

### Key Integrations

| Integration | Purpose |
|---|---|
| **Jira** | Task source — backlog management and task status updates |
| **GitHub** | Code repository, branch management, pull request lifecycle |
| **WhatsApp / Slack** | Engineer notifications and optional approval triggers |
| **Sandbox Environments** | Isolated execution and testing of AI-generated changes |

### Future Enhancement: On-Demand Sandbox Provisioning

- In the current POC, sandbox environments are pre-provisioned
- The roadmap includes **automatic sandbox creation at task start**, making the system fully dynamic and resource-efficient
- Each task would receive a clean, isolated environment for the full duration of its lifecycle

---

## 7. Architecture Overview — Event-Driven Execution

### The Core Architectural Choice: Events, Not Loops

The most important architectural decision in this platform is the rejection of **continuous agent loops** in favour of **event-driven, task-scoped execution**.

#### Continuous Loop (Antfarm / OpenClaw model — avoided)

```
Agent starts → polls for work → processes → polls again → processes → polls again → ...
              [token cost]    [token cost]  [token cost]  [token cost]  [token cost]
```

- Agent maintains a long-running context window, accumulating cost over time
- Polling cycles consume tokens even when there is no work to do
- A misbehaving or confused agent continues to consume resources indefinitely
- Failure modes are difficult to bound and reason about

#### Event-Driven Execution (Our approach)

```
Task arrives → Platform dispatches agent → Agent executes → Agent exits → Resources released
              [one bounded execution]                      [cost stops here]
```

- An agent is spawned **only when a task is ready to be worked on**
- The agent has a **clearly scoped mission**: implement this specific task, run tests, open a PR
- Once the task is complete (or fails), the agent session **terminates and all resources are released**
- **Token cost is bounded per task** — there is no open-ended loop

### How This Reduces Token Usage

| Source of Token Waste | Continuous Loop | Event-Driven |
|---|---|---|
| Idle polling between tasks | High | None |
| Redundant context re-loading | High | None — fresh context per task |
| Long-running context window growth | Accumulates | Bounded to task scope |
| Failed tasks continuing to retry | Possible | Controlled retry with limits |

### Deterministic Steps Avoid LLM Calls Entirely

Where the workflow can be handled deterministically, we avoid LLM inference entirely:

- **Task routing** — rule-based selection of eligible tasks from the queue (no LLM)
- **Environment provisioning** — scripted, templated setup (no LLM)
- **Test execution** — standard CI runner commands (no LLM)
- **PR creation** — structured GitHub API calls using agent-generated summary (minimal LLM)
- **Notification dispatch** — webhook-based messaging (no LLM)

Only the **core implementation work** — understanding the task, exploring the codebase, writing code, fixing tests — requires LLM inference. Everything else is handled by the platform using conventional software.

---

## 8. Orchestration Design

### Queue-Based Task Execution

- All tasks eligible for agent handling enter a **managed task queue** within the platform
- The queue is the single source of truth for what is pending, in-progress, and complete
- Tasks are never dispatched to more agents than the platform has capacity to run safely

### Task Prioritisation

Tasks in the queue are prioritised using a scoring model based on:

| Priority Factor | Rationale |
|---|---|
| Jira priority label (Critical / High / Medium / Low) | Reflects business-defined urgency |
| Task age — how long it has been in the backlog | Prevents low-priority tasks from being starved indefinitely |
| Task complexity estimate | Simpler tasks are preferred early in the POC to maximise success rate |
| Team or project assignment | Enables per-team quotas and fair scheduling |

### Retry and Failure Handling

- Each task is given a **maximum number of agent attempts** (e.g. 3 retries before escalation)
- If an agent session fails (test failures, implementation error, timeout), the task is returned to the queue with a failure annotation
- After exhausting retries, the task is **escalated to a human engineer** via notification with the agent's last output attached for diagnostic context
- Failures are classified to distinguish **recoverable errors** (e.g. flaky test) from **permanent failures** (e.g. task is ambiguous or out of scope)

### Execution Limits and Circuit Breakers

- Each agent session has a **wall-clock time limit** — sessions that run beyond this threshold are terminated
- A **token budget** is enforced per task — if the agent approaches the limit without completing, the session is gracefully wound down and the task is flagged for review
- A **global circuit breaker** monitors overall agent success rate — if the rate drops below a threshold (e.g. more than 50% of tasks failing in a window), the platform pauses new dispatches and alerts the engineering team
- These controls ensure that **a surge in failures does not silently drain resources**

---

## 9. Multi-Task Execution

### Running Multiple Tasks Concurrently

- The platform is designed to support **multiple AI agents operating in parallel**
- Each task runs in its own **isolated git worktree**, preventing agents from interfering with one another
- Background execution ensures tasks do not block each other or require sequential processing

### Parallel Execution Model

```
Task A ──→ Agent 1 ──→ Worktree A ──→ Sandbox A ──→ PR #101
Task B ──→ Agent 2 ──→ Worktree B ──→ Sandbox B ──→ PR #102
Task C ──→ Agent 3 ──→ Worktree C ──→ Sandbox C ──→ PR #103
```

### Benefits of Parallel Execution

- **Throughput scales with task volume** — the platform can process many tasks simultaneously
- Failures in one task do not affect others
- Engineers receive multiple PRs ready for review rather than waiting for tasks to complete sequentially
- Resource allocation can be tuned based on infrastructure capacity and priority

---

## 10. Sandbox Strategy — Trade-offs and Cost Analysis

### Why Sandboxes Are Necessary

- AI agents must be able to **run tests, start services, and execute code** as part of validating their changes
- Running this against shared development or staging environments creates **unacceptable risk** — a broken test or misconfigured service could affect other engineers
- **Isolated sandbox environments** contain all execution within a boundary, ensuring failures have no blast radius beyond the task itself

### The Core Trade-off: Per-Task vs Shared Environments

#### Option A: Dedicated Sandbox Per Task

```
Task A → Sandbox A (full stack)  ─┐
Task B → Sandbox B (full stack)  ─┼─ Each fully isolated
Task C → Sandbox C (full stack)  ─┘
```

**Advantages:**
- Complete isolation — no risk of task interference
- Environments can be customised precisely to the task's service dependencies
- Clean teardown — environment is discarded after the task, with no residual state

**Disadvantages:**
- **High infrastructure cost** — spinning up a full service stack per task is expensive
- Slow provisioning — startup time adds latency to the overall task cycle
- For microservices architectures, recreating the **full service topology** (databases, queues, dependent services) is complex and resource-intensive

#### Option B: Shared Baseline Environments with Task Isolation

```
Shared Base (databases, infrastructure services)
    ├── Task A namespace (isolated application layer)
    ├── Task B namespace (isolated application layer)
    └── Task C namespace (isolated application layer)
```

**Advantages:**
- Significantly **reduced infrastructure cost** — shared components are provisioned once
- Faster task startup — base environment is already running
- More scalable — many tasks can share a common infrastructure layer

**Disadvantages:**
- More complex to implement correctly — namespace isolation must be strictly enforced
- Shared components (e.g. database) can become a **bottleneck or failure point**
- Requires careful network routing to prevent cross-task data contamination

### Our Approach: Hybrid Model

- **POC Phase:** Dedicated environments per task — prioritise simplicity and safety over cost efficiency
- **Pilot Phase:** Transition to shared baseline with isolated application namespaces, reducing per-task cost
- **Scale Phase:** Dynamic provisioning — full isolation for high-risk tasks, shared baseline for low-risk routine tasks

### Infrastructure Cost Considerations

| Environment Model | Estimated Relative Cost | Isolation Level | Provisioning Speed |
|---|---|---|---|
| Full dedicated stack per task | High | Maximum | Slow |
| Shared base + isolated namespace | Medium | High | Fast |
| Shared environment, agent-scoped | Low | Moderate | Very fast |

- Cost scales with **concurrency** — the more tasks running simultaneously, the more environments are active
- Automatic teardown on task completion is **critical** — orphaned environments are a primary cost leak risk
- The platform enforces **maximum concurrent task limits** to prevent unbounded infrastructure spend

---

## 11. Responsibility Boundaries

### Clear Separation of Concerns

This platform has a deliberate division between what the **orchestration platform** manages and what the **AI coding agent** is responsible for.

#### System Platform (Foreman / Orchestration Layer)

- Task queue management — selecting, prioritising, and tracking tasks
- Agent lifecycle management — spawning, monitoring, and terminating agent sessions
- Sandbox environment provisioning and teardown
- Integration with Jira, GitHub, and messaging systems
- Security controls — access management, resource limits, and environment isolation
- Observability — logging, monitoring, and alerting on agent activity
- Cost management — tracking and controlling infrastructure usage

#### AI Coding Agent (Claude Code)

- Reading and understanding task requirements
- Exploring the codebase to identify relevant files and context
- Writing and modifying code to implement the required change
- Running automated tests to validate correctness
- Fixing test failures and iterating on the implementation
- Writing clear pull request descriptions and changelogs
- Updating inline documentation and comments where relevant

### Why This Separation Matters

- It ensures the AI agent remains **focused on code** while the platform handles all operational concerns
- It allows the platform to work with **different AI agents** over time as the technology evolves
- It provides **clear accountability** — the platform is responsible for safety and orchestration; the agent is responsible for code quality

---

## 12. Next Steps for the Platform (SessionUp)

### Immediate Priorities for POC Stabilisation

The POC cannot be reliably validated without a strong automated testing foundation. Key priorities:

- **Stabilise unit tests** — ensure all core platform components have reliable, repeatable unit tests
- **Stabilise end-to-end (E2E) tests** — validate complete workflows from task intake to PR creation
- **Agent test execution** — confirm that AI agents can reliably run the test suite and interpret results correctly
- **Failure handling** — ensure agents can identify test failures, diagnose issues, and attempt fixes autonomously

### Why Testing Infrastructure Comes First

- Agents must be able to **validate their own changes** before submitting them for human review
- Without reliable tests, agents cannot determine whether their implementation is correct
- A robust test suite is the **safety net** that makes autonomous code changes trustworthy
- It also reduces the review burden on engineers — a PR that passes all tests requires less scrutiny

### Code Quality and Reliability Goals

- Improve overall system reliability to support longer, uninterrupted agent sessions
- Establish baseline metrics for agent task success rate, PR acceptance rate, and test pass rate
- Ensure the platform can recover gracefully from agent failures or unexpected errors

---

## 13. Current Challenges and Risks

### Technical Challenges

#### Infrastructure Overhead
- Creating a **dedicated sandbox environment per task** (or per PR) consumes significant infrastructure resources
- At scale, running dozens of concurrent environments simultaneously requires careful capacity planning
- **Risk:** Infrastructure costs could grow faster than expected if not carefully managed

#### Networking Complexity
- Many of the applications being developed are **distributed microservices**
- Running each task in an isolated sandbox means recreating the full service topology within that environment
- **Routing and service discovery** across many parallel environments is technically complex
- **Risk:** Misconfigured networking could cause test failures that do not reflect real code issues

#### Agent Reliability
- AI agents can occasionally produce incorrect code, misunderstand requirements, or enter loops
- Without proper guardrails, a misbehaving agent could consume excessive resources or open many low-quality PRs
- **Risk:** Requires robust monitoring, timeout controls, and circuit breakers

#### Security Considerations
- AI agents require code repository access and the ability to run commands in sandbox environments
- Access controls must be strictly scoped to prevent agents from accessing unintended systems
- All agent activity must be logged for auditability

### Mitigation Strategies

| Challenge | Mitigation |
|---|---|
| Infrastructure cost | Shared base environments, on-demand provisioning, automatic teardown |
| Networking complexity | Standardised environment templates, service mesh tooling |
| Agent reliability | Timeouts, retry limits, human escalation paths |
| Security | Least-privilege access, network isolation, comprehensive audit logging |

---

## 14. Proposed Roadmap

### Phase 1 — POC Stabilisation *(Current Phase)*

**Goal:** Validate the core concept with a reliable, testable foundation

- Stabilise unit and E2E test infrastructure
- Validate the core orchestration workflow end-to-end
- Run limited internal experiments with real tasks
- Measure baseline metrics: task success rate, time-to-PR, test pass rate
- Identify and resolve critical blockers

**Success Criteria:** At least one real engineering task completed autonomously end-to-end, with a passing test suite and a mergeable PR

---

### Phase 2 — Internal Pilot

**Goal:** Expand to a small set of real engineering tasks with selected team members

- Full Jira and GitHub integration automated
- Agents able to create PRs automatically with meaningful descriptions
- Sandbox execution enabled for a curated set of task types (e.g. bug fixes, test additions)
- Engineers notified via messaging channel with PR links
- Feedback loop established — engineers rate PR quality and flag issues

**Success Criteria:** 10+ real tasks completed, majority resulting in merged or near-mergeable PRs

---

### Phase 3 — Scaled Engineering Support

**Goal:** Increase throughput and system robustness for broader team use

- Multi-agent parallel task execution in production
- Improved sandbox orchestration with on-demand environment provisioning
- Better observability — dashboards showing agent activity, task queue status, and success rates
- Cost controls and resource quotas per project or team
- Support for a wider range of task types

**Success Criteria:** Platform reliably processing 20+ tasks per week across multiple agents and projects

---

### Phase 4 — Autonomous Engineering Platform *(Long-Term Vision)*

**Goal:** A self-sustaining engineering support layer integrated into the standard development workflow

- Agents proactively identifying and proposing tasks from the backlog without manual intervention
- Automatic environment lifecycle management — provision, execute, validate, teardown
- AI-assisted continuous development workflow — agents iterating on tasks based on code review feedback
- Integration with CI/CD pipelines for seamless deployment of approved changes
- Comprehensive reporting for engineering leadership — throughput, quality, cost per task

**Success Criteria:** Measurable, sustained reduction in engineering backlog growth rate

---

## 15. Cost Control Strategy

### The Cost Problem at Scale

- AI-assisted development introduces **two new categories of operational cost**: LLM token consumption and sandbox infrastructure
- Without deliberate controls, both costs can grow non-linearly as task volume and agent concurrency increase
- The lessons from Antfarm / OpenClaw make this clear — continuous agent loops can generate costs that far outpace the value delivered
- **Cost predictability is a requirement**, not a nice-to-have, for sustainable deployment

### Token Budget Controls

- Every agent task is assigned a **hard token budget** — the maximum number of tokens (input + output) that may be consumed for that task
- The budget is sized based on task type and complexity estimate — a simple bug fix has a smaller budget than a larger refactor
- As a task's token consumption approaches the budget ceiling, the platform **warns the agent** to conclude its work
- Tasks that exceed their budget are **terminated and flagged** — the partial work is preserved and reviewed, and the budget is adjusted for future similar tasks
- This prevents a single confused or looping agent from consuming disproportionate resources

### Minimising LLM Calls

The platform is designed to route work through LLM inference **only when it is genuinely required**:

| Step | Handled By |
|---|---|
| Task selection and queue management | Deterministic platform logic — no LLM |
| Environment provisioning | Scripted templates — no LLM |
| Running tests and capturing results | CI runner — no LLM |
| Routing notifications and PR creation | API calls with structured templates — minimal LLM |
| Code implementation and test debugging | Claude Code (LLM) — this is the value-add |
| Failure classification and escalation | Rule-based classification — no LLM |

The goal is that **LLM inference is concentrated on the one step that benefits from it**: writing code. All surrounding workflow steps are handled with conventional software.

### Deterministic Workflows Where Possible

- Well-defined tasks with structured inputs (e.g. "add a unit test for function X") can be executed with **highly constrained agent prompts** — reducing ambiguity and unnecessary exploration
- The platform pre-processes Jira task descriptions to extract structured fields (affected files, expected behaviour, acceptance criteria) before passing them to the agent — reducing the amount of context the agent must infer itself
- Standardised **task templates** guide agents toward predictable execution paths, reducing token-intensive trial-and-error

### Infrastructure Cost Controls

- **Concurrent environment cap**: the platform enforces a maximum number of simultaneously active sandbox environments
- **Automatic teardown**: environments are decommissioned immediately after task completion or failure — no idle environments
- **Tiered environment sizing**: low-complexity tasks use smaller, cheaper sandbox configurations; only complex tasks requiring full service stacks get larger environments
- **Cost attribution per team / project**: infrastructure and token costs are tracked and reported per team, enabling informed decisions about task eligibility and agent usage

### Cost Observability

- A real-time **cost dashboard** tracks token consumption and infrastructure spend per task, per project, and per time period
- **Alerts** are triggered if per-task average cost exceeds expected ranges, indicating potential agent misbehaviour
- Monthly cost reports are produced for engineering leadership to assess ROI against tasks completed and backlog reduction

---

## 16. Strategic Value and Business Case

### Why This Matters to the Business

- **Engineering capacity is finite** — headcount growth is expensive and slow; AI assistance scales differently
- **Backlog debt compounds** — unaddressed small tasks accumulate technical debt that slows future development
- **Faster iteration cycles** — reducing the time from "task identified" to "change deployed" accelerates product delivery
- **Developer satisfaction** — removing repetitive, low-value tasks allows engineers to work on more meaningful problems

### Return on Investment Indicators

| Metric | Expected Improvement |
|---|---|
| Time to resolve routine backlog items | Significantly reduced |
| Engineer hours spent on low-complexity tasks | Reduced, redirected to higher-value work |
| PR cycle time for small changes | Reduced |
| Backlog growth rate | Slowed over time as autonomous capacity increases |

### Positioning

- This POC positions the organisation as an **early adopter** of autonomous engineering tooling
- The knowledge and infrastructure built here will be a **strategic asset** as AI-assisted development becomes industry standard
- Internal capability means we are not dependent on third-party platforms for this workflow

---

## 17. Summary

### What We Are Building

An **AI-Assisted Development Platform** that allows coding agents to operate continuously, pick up engineering tasks from the backlog, implement changes in isolated sandbox environments, and submit pull requests for human review — all without manual initiation.

### What Makes This Different

- **Continuous, not on-demand** — agents work around the clock, not just when triggered
- **Safe by design** — all changes are sandboxed, tested, and reviewed before merging
- **Human oversight retained** — engineers remain in the loop for all code that enters the codebase
- **Composable architecture** — the platform is designed to work with current and future AI coding agents

### The Ask

- **Endorsement** to continue POC development through Phase 1 stabilisation
- **Resource allocation** for sandbox infrastructure and engineering time
- **Alignment** on success criteria and review cadence for the internal pilot

### The Opportunity

> By giving AI agents the infrastructure to work continuously and safely, we can meaningfully expand our engineering throughput without a proportional increase in headcount — and build the foundation for how engineering teams will work in the next decade.

---

*Document prepared for Board of Directors review — March 2026*
*Classification: Internal — Confidential*
