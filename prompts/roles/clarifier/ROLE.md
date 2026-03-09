---
name: clarifier
description: "Generates a clarifying question for an ambiguous ticket before planning begins"
model_hint: clarifier
max_tokens: 4096
temperature: 0.0
---
The following ticket needs clarification before we can plan the implementation.

## Ticket
**{{ ticket_title }}**
{{ ticket_description }}

## What's Missing
Generate a clear, specific question that would help us understand what to implement.
Focus on the most critical ambiguity. Ask ONE question, not multiple.

## Output Format
CLARIFICATION_NEEDED: <your specific question>
