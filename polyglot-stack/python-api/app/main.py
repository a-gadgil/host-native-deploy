import json
import os
import time
from decimal import Decimal
from typing import Any

import psycopg
import redis
from fastapi import FastAPI, HTTPException, Response
from pydantic import BaseModel, Field

START_TIME = time.time()

APP_ENV = os.getenv("APP_ENV", "host-native")
DATABASE_URL = os.getenv(
    "DATABASE_URL",
    "postgresql://polyglot_app:polyglot_dev_pass@127.0.0.1:5432/polyglot_db",
)
REDIS_URL = os.getenv("REDIS_URL", "redis://127.0.0.1:6379/0")

app = FastAPI(title="polyglot-python-api", version="1.0.0")
redis_client = redis.Redis.from_url(
    REDIS_URL, decode_responses=True, socket_connect_timeout=1.0, socket_timeout=1.0
)


class ScoreRequest(BaseModel):
    customer_id: str = Field(..., min_length=1, max_length=64)
    amount_cents: int = Field(..., gt=0)


class ScoreResponse(BaseModel):
    customer_id: str
    recent_orders: int
    risk_score: float
    risk_tier: str


@app.get("/healthz")
def healthz() -> dict[str, Any]:
    return {
        "status": "ok",
        "service": "python-api",
        "env": APP_ENV,
        "uptime_seconds": int(time.time() - START_TIME),
    }


@app.get("/readyz")
def readyz() -> dict[str, str]:
    try:
        with psycopg.connect(DATABASE_URL, connect_timeout=2) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT 1;")
                cur.fetchone()
        redis_client.ping()
    except Exception as exc:
        raise HTTPException(status_code=503, detail=f"dependency not ready: {exc}") from exc

    return {"status": "ready", "service": "python-api"}


@app.post("/internal/v1/score", response_model=ScoreResponse)
def compute_risk_score(payload: ScoreRequest) -> ScoreResponse:
    recent_orders = 0
    try:
        with psycopg.connect(DATABASE_URL, connect_timeout=2) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    SELECT COUNT(*)
                    FROM orders
                    WHERE customer_id = %s
                      AND created_at >= NOW() - INTERVAL '24 hours';
                    """,
                    (payload.customer_id,),
                )
                row = cur.fetchone()
                recent_orders = int(row[0]) if row else 0
    except Exception as exc:
        raise HTTPException(status_code=503, detail=f"database query failed: {exc}") from exc

    # Base score from order amount + velocity penalty for repeat 24h orders
    amount_dollars = payload.amount_cents / 100.0
    score = min(99.99, round((amount_dollars / 100.0) + (recent_orders * 15.0), 2))

    if score >= 70.0:
        tier = "HIGH"
    elif score >= 35.0:
        tier = "MEDIUM"
    else:
        tier = "LOW"

    return ScoreResponse(
        customer_id=payload.customer_id,
        recent_orders=recent_orders,
        risk_score=score,
        risk_tier=tier,
    )


@app.get("/api/v1/analytics/summary")
def analytics_summary(response: Response) -> dict[str, Any]:
    cache_key = "analytics:summary"

    # 1. Cache-aside lookup in Redis
    try:
        cached = redis_client.get(cache_key)
        if cached:
            response.headers["X-Cache"] = "HIT"
            return json.loads(cached)
    except redis.RedisError:
        pass  # Degrade gracefully to PostgreSQL if Redis is unreachable

    # 2. Aggregate query against PostgreSQL
    try:
        with psycopg.connect(DATABASE_URL, connect_timeout=2) as conn:
            with conn.cursor() as cur:
                cur.execute(
                    """
                    SELECT
                        COUNT(*) AS total_orders,
                        COALESCE(SUM(amount_cents), 0) AS total_amount_cents,
                        COALESCE(ROUND(AVG(risk_score), 2), 0) AS avg_risk_score
                    FROM orders;
                    """
                )
                totals = cur.fetchone()

                cur.execute(
                    """
                    SELECT risk_tier, COUNT(*)
                    FROM orders
                    GROUP BY risk_tier
                    ORDER BY risk_tier;
                    """
                )
                tier_rows = cur.fetchall()
    except Exception as exc:
        raise HTTPException(status_code=503, detail=f"analytics query failed: {exc}") from exc

    summary = {
        "total_orders": int(totals[0]) if totals else 0,
        "total_amount_cents": int(totals[1]) if totals else 0,
        "avg_risk_score": float(totals[2]) if isinstance(totals[2], Decimal) else 0.0,
        "by_risk_tier": {row[0]: int(row[1]) for row in tier_rows},
    }

    # 3. Populate Redis cache for 30 seconds
    try:
        redis_client.setex(cache_key, 30, json.dumps(summary))
    except redis.RedisError:
        pass

    response.headers["X-Cache"] = "MISS"
    return summary
