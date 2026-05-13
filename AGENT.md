# CrossPost: Project & AI Agent Instructions
DO NOT READ .env

## Project Description
**CrossPost** is a cross-platform social scheduler built for freelancers, small business owners, and content creators. 
* **The Problem:** Small businesses need to post updates on LinkedIn, Twitter/X, and Facebook simultaneously without paying expensive monthly fees for tools like Hootsuite.
* **The Product:** A web app where users write one post, attach an image, select a date/time, and the system automatically posts it across all their connected social media accounts at the exact right moment.
* **The Tech Stack:** Go, PostgreSQL (GORM), Redis (Asynq for message queues), Docker, and eventually AWS (ECS, RDS, ElastiCache).

## User Profile: Beginner Learner First
- **I am a beginner coder.** My primary goal is to **learn and understand** backend architecture, not just to get a working app as fast as possible.
- **Explain before implementing:** Before generating code for a new feature, explain the underlying concept in plain English (e.g., how Go handles concurrency, how the OAuth 2.0 flow works, or why we need a Redis queue). Wait for my understanding before proceeding.
- **Cost conscious:** Do not let me overuse my AWS resources or AI API rates so I have to pay extra money. Prioritize local Docker development and AWS Free Tier solutions.

## Non-Negotiables
- Make SMALL, incremental changes. Do not rewrite architecture without updating documentation.
- Always add or update Go tests (`*_test.go`) for new behavior.
- Always run: `go test ./... -v` before finishing a task.
- If tests fail, fix them. Do not leave the repo in a broken state.
- Never commit secrets. Use local environment variables (`.env`) and keep a `.env.example` in the repo.
- This repo uses `.gitignore`. Do not add ignored files or suggest committing them.

## Output Requirements
- Prefer returning a unified diff (patch) when asked to modify code.
- Keep changes strictly scoped to the current ticket or task.
- Use strictly typed Go `structs` for database models, API requests, and tool I/O.

## Safety Requirements
- Any “publishing/posting” action to external APIs (like Twitter or LinkedIn) must use a two-phase commit:
  1. Draft the post in the PostgreSQL database and enqueue the job in Redis.
  2. Require explicit validation that the target time has been reached before the Go worker executes the external API call.

## Quality Bar
- **Engineering Preferences:**
  - **DRY is important:** Flag repetition aggressively.
  - **Well-tested code is non-negotiable:** I would rather have too many tests than too few.
  - **"Engineered enough":** Avoid under-engineering (fragile, hacky code) and over-engineering (premature abstractions, unnecessary complexity).
  - **Edge cases over speed:** I err on the side of handling more edge cases; thoughtfulness > speed.
  - **Explicit > Clever:** Use standard, idiomatic Go. Avoid clever one-liners that are hard for a beginner to read.
- Deterministic evals: Unit tests must not depend on live web calls (mock external APIs).

## Review & Implementation Workflow
Review proposed plans thoroughly before making any code changes. For every issue or recommendation, explain the concrete tradeoffs, give me an opinionated recommendation, and ask for my input before assuming a direction.

### Review Stages
1. **Architecture review:** Component boundaries, dependency graphs, data flow, single points of failure, and security (OAuth, API boundaries).
2. **Code quality review:** Go module structure, DRY violations, error handling patterns (explicitly call out missing `if err != nil` checks), and technical debt.
3. **Test review:** Coverage gaps, assertion strength, and untracked failure modes.
4. **Performance review:** Database query efficiency, Goroutine memory leaks, and blocking code paths.

### Issue Formatting & Interaction Protocol
Do not assume my priorities on timeline or scale. After each section, pause and ask for my feedback before moving on. 

**BEFORE YOU START A REVIEW, ASK:**
"Do you want one of two options:
1/ **BIG CHANGE:** Work through this interactively, one section at a time (Architecture → Code Quality → Tests → Performance) with at most 4 top issues in each section.
2/ **SMALL CHANGE:** Work through interactively ONE question per review section."

**FOR EACH ISSUE IDENTIFIED:**
Describe the problem concretely, with file and line references. Present 2-3 options, including "do nothing" where reasonable. Output the explanation and pros and cons of each option AND your opinionated recommendation and why.

**STRICT FORMATTING FOR OPTIONS:**
1. Number each issue (e.g., **Issue 1**, **Issue 2**).
2. Assign letters to options (e.g., **Option A**, **Option B**). 
3. **Option A** MUST always be your recommended option.
4. Use `AskUserQuestion` to halt execution. Make sure each prompt clearly labels the issue NUMBER and option LETTER so I do not get confused (e.g., *"For Issue 1, do you prefer Option A (Recommended) or Option B?"*).