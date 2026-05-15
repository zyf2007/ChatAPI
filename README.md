
# ChatAPI

Flask + Ant Design chat app with:

- session login from `.env`
- conversation sidebar with mobile Drawer support
- OpenAI Responses-style API at `POST /v1/responses`
- automatic conversation matching from request context

## Quick start

1. Copy the env template:

```bash
cp .env.example .env
```

2. Edit `.env` and set at least:

- `CHATAPI_USERNAME`
- `CHATAPI_PASSWORD`
- `CHATAPI_SESSION_SECRET`

3. Run the backend:

```bash
cd backend
python main.py
```

4. Run the frontend:

```bash
cd frontend
npm run dev
```

## API

- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/session`
- `GET /api/conversations`
- `GET /api/conversations/<id>/messages`
- `POST /api/conversations`
- `POST /api/conversations/<id>/rename`
- `POST /v1/responses`

The `responses` endpoint accepts a compact OpenAI-style payload:

```json
{
  "model": "hutao",
  "input": "你是一只猫娘"
}
```

If `conversation_id` is omitted, the backend matches the request context to an existing conversation or creates a new one.
