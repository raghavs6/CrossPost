# CrossPost

CrossPost is a cross-platform social media scheduling app built to automate posting across multiple networks (Twitter/X, LinkedIn, Facebook). 

**The core purpose of this repository is pedagogical.** It is designed as a laboratory to learn a modern, decoupled architecture: a React SPA frontend communicating with a highly concurrent Go backend.

---

## 🛠️ Tech Stack

**Frontend (Single Page Application):**
* **Framework:** React + Vite
* **Data Fetching:** Standard REST API calls to the Go backend.

**Backend (API & Workers):**
* **Language:** Go (Golang)
* **Database:** PostgreSQL (via GORM)
* **Message Broker / Queues:** Redis & Asynq
* **Infrastructure:** Docker & Docker Compose

---

## 📂 Repository Structure

```text
crosspost/
├── frontend/                    # React + Vite SPA
├── cmd/
│   └── api/
│       └── main.go              # Go Backend entry point
├── internal/                    # Core Go logic (Auth, Database, Workers)
├── docker-compose.yml           # Local infrastructure (Postgres + Redis)
├── CLAUDE.md                    # AI Agent Guardrails
└── README.md                    # Project documentation

## Local Auth Setup

Google sign-in uses the backend OAuth routes, so the server needs these
variables in the repo-level `.env`:

- `GOOGLE_CLIENT_ID`
- `GOOGLE_CLIENT_SECRET`
- `GOOGLE_REDIRECT_URL=http://localhost:8080/api/auth/google/callback`

In Google Cloud Console, create a Web application OAuth client and add this
exact authorised redirect URI:

- `http://localhost:8080/api/auth/google/callback`

If the app shows `Error 401: invalid_client`, the most likely cause is that the
client ID in `.env` does not match an existing Google OAuth client.
