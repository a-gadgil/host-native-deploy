use axum::{
    extract::State,
    http::{header, HeaderMap, HeaderValue, StatusCode},
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use redis::AsyncCommands;
use serde::{Deserialize, Serialize};
use sqlx::{mysql::MySqlPoolOptions, MySqlPool, Row};
use std::{
    collections::BTreeMap,
    env,
    io::{Read, Write},
    net::{SocketAddr, TcpStream},
    process,
    sync::Arc,
    time::{Duration, Instant},
};
use tokio::signal;

const CACHE_KEY: &str = "analytics:summary";

#[derive(Clone)]
struct AppState {
    db: MySqlPool,
    redis_client: redis::Client,
    app_env: String,
    started_at: Instant,
}

#[derive(Deserialize)]
struct ScoreRequest {
    customer_id: String,
    amount_cents: i64,
}

#[derive(Serialize)]
struct ScoreResponse {
    customer_id: String,
    recent_orders: i64,
    risk_score: f64,
    risk_tier: &'static str,
}

#[derive(Serialize, Deserialize)]
struct AnalyticsSummary {
    total_orders: i64,
    total_amount_cents: i64,
    avg_risk_score: f64,
    by_risk_tier: BTreeMap<String, i64>,
}

type ApiError = (StatusCode, Json<serde_json::Value>);
type ApiJsonResult = Result<(StatusCode, HeaderMap, Json<serde_json::Value>), ApiError>;

fn run_healthcheck(host: &str, port: &str) -> ! {
    let connect_host = if host == "0.0.0.0" { "127.0.0.1" } else { host };
    let addr = format!("{}:{}", connect_host, port);
    match TcpStream::connect(&addr) {
        Ok(mut stream) => {
            let _ = stream.set_read_timeout(Some(Duration::from_secs(2)));
            let _ = stream.set_write_timeout(Some(Duration::from_secs(2)));
            let req = format!(
                "GET /healthz HTTP/1.1\r\nHost: {}\r\nConnection: close\r\n\r\n",
                connect_host
            );
            if stream.write_all(req.as_bytes()).is_ok() {
                let mut buf = [0u8; 64];
                if let Ok(n) = stream.read(&mut buf) {
                    let head = String::from_utf8_lossy(&buf[..n]);
                    if head.starts_with("HTTP/1.1 200") || head.starts_with("HTTP/1.0 200") {
                        process::exit(0);
                    }
                }
            }
            process::exit(1);
        }
        Err(_) => process::exit(1),
    }
}

#[tokio::main]
async fn main() {
    let app_host = env::var("APP_HOST").unwrap_or_else(|_| "127.0.0.1".to_string());
    let app_port = env::var("APP_PORT").unwrap_or_else(|_| "8091".to_string());

    if env::args().any(|a| a == "--healthcheck") {
        run_healthcheck(&app_host, &app_port);
    }

    let app_env = env::var("APP_ENV").unwrap_or_else(|_| "host-native".to_string());
    let database_url = env::var("MARIADB_URL").unwrap_or_else(|_| {
        "mysql://polyglot_app:polyglot_dev_pass@127.0.0.1:3306/polyglot_mariadb_db?ssl-mode=DISABLED"
            .to_string()
    });
    let redis_url = env::var("REDIS_URL").unwrap_or_else(|_| "redis://127.0.0.1:6379/".to_string());

    let db = MySqlPoolOptions::new()
        .max_connections(15)
        .acquire_timeout(Duration::from_secs(3))
        .connect(&database_url)
        .await
        .expect("failed to connect to MariaDB");

    let redis_client = redis::Client::open(redis_url).expect("invalid REDIS_URL");

    let state = Arc::new(AppState {
        db,
        redis_client,
        app_env: app_env.clone(),
        started_at: Instant::now(),
    });

    let app = Router::new()
        .route("/healthz", get(healthz))
        .route("/readyz", get(readyz))
        .route("/internal/v1/score", post(compute_risk_score))
        .route("/api/v1/analytics/summary", get(analytics_summary))
        .with_state(state);

    let bind_addr: SocketAddr = format!("{}:{}", app_host, app_port)
        .parse()
        .expect("invalid APP_HOST:APP_PORT");

    println!(
        "polyglot-rust-api listening on http://{} (env={})",
        bind_addr, app_env
    );

    let listener = tokio::net::TcpListener::bind(bind_addr)
        .await
        .expect("failed to bind TCP listener");

    axum::serve(listener, app)
        .with_graceful_shutdown(shutdown_signal())
        .await
        .expect("server error");

    println!("polyglot-rust-api shutdown complete");
}

async fn shutdown_signal() {
    let ctrl_c = async {
        signal::ctrl_c()
            .await
            .expect("failed to install Ctrl+C handler");
    };

    #[cfg(unix)]
    let terminate = async {
        signal::unix::signal(signal::unix::SignalKind::terminate())
            .expect("failed to install SIGTERM handler")
            .recv()
            .await;
    };

    #[cfg(not(unix))]
    let terminate = std::future::pending::<()>();

    tokio::select! {
        _ = ctrl_c => {},
        _ = terminate => {},
    }
}

async fn healthz(State(state): State<Arc<AppState>>) -> Json<serde_json::Value> {
    Json(serde_json::json!({
        "status": "ok",
        "service": "rust-api",
        "env": state.app_env,
        "uptime_seconds": state.started_at.elapsed().as_secs()
    }))
}

async fn readyz(State(state): State<Arc<AppState>>) -> impl IntoResponse {
    if let Err(err) = sqlx::query("SELECT 1").execute(&state.db).await {
        return (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "status": "not_ready",
                "error": format!("mariadb unreachable: {}", err)
            })),
        );
    }

    match state.redis_client.get_multiplexed_async_connection().await {
        Ok(mut conn) => {
            let pong: Result<String, _> = redis::cmd("PING").query_async(&mut conn).await;
            if pong.map(|p| p == "PONG").unwrap_or(false) {
                return (
                    StatusCode::OK,
                    Json(serde_json::json!({
                        "status": "ready",
                        "service": "rust-api"
                    })),
                );
            }
        }
        Err(err) => {
            return (
                StatusCode::SERVICE_UNAVAILABLE,
                Json(serde_json::json!({
                    "status": "not_ready",
                    "error": format!("redis unreachable: {}", err)
                })),
            );
        }
    }

    (
        StatusCode::SERVICE_UNAVAILABLE,
        Json(serde_json::json!({
            "status": "not_ready",
            "error": "redis PING failed"
        })),
    )
}

async fn compute_risk_score(
    State(state): State<Arc<AppState>>,
    Json(payload): Json<ScoreRequest>,
) -> Result<Json<ScoreResponse>, ApiError> {
    if payload.customer_id.trim().is_empty() || payload.amount_cents <= 0 {
        return Err((
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({
                "error": "customer_id and positive amount_cents are required"
            })),
        ));
    }

    let row = sqlx::query(
        "SELECT COUNT(*) AS cnt FROM orders WHERE customer_id = ? AND created_at >= DATE_SUB(UTC_TIMESTAMP(6), INTERVAL 24 HOUR)",
    )
    .bind(&payload.customer_id)
    .fetch_one(&state.db)
    .await
    .map_err(|err| {
        (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": format!("database query failed: {}", err)
            })),
        )
    })?;

    let recent_orders: i64 = row.try_get("cnt").unwrap_or(0);
    let amount_dollars = (payload.amount_cents as f64) / 100.0;
    let raw_score = (amount_dollars / 100.0) + (recent_orders as f64 * 15.0);
    let risk_score = ((raw_score * 100.0).round() / 100.0).min(99.99);

    let risk_tier = if risk_score >= 70.0 {
        "HIGH"
    } else if risk_score >= 35.0 {
        "MEDIUM"
    } else {
        "LOW"
    };

    Ok(Json(ScoreResponse {
        customer_id: payload.customer_id,
        recent_orders,
        risk_score,
        risk_tier,
    }))
}

async fn analytics_summary(State(state): State<Arc<AppState>>) -> ApiJsonResult {
    if let Ok(mut conn) = state.redis_client.get_multiplexed_async_connection().await {
        if let Ok(Some(cached)) = conn.get::<_, Option<String>>(CACHE_KEY).await {
            if let Ok(val) = serde_json::from_str::<serde_json::Value>(&cached) {
                let mut headers = HeaderMap::new();
                headers.insert(
                    header::HeaderName::from_static("x-cache"),
                    HeaderValue::from_static("HIT"),
                );
                return Ok((StatusCode::OK, headers, Json(val)));
            }
        }
    }

    let totals_row = sqlx::query(
        "SELECT COUNT(*) AS total_orders, CAST(COALESCE(SUM(amount_cents), 0) AS SIGNED) AS total_amount_cents, CAST(COALESCE(ROUND(AVG(risk_score), 2), 0.00) AS DOUBLE) AS avg_risk_score FROM orders",
    )
    .fetch_one(&state.db)
    .await
    .map_err(|err| {
        (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": format!("analytics query failed: {}", err)
            })),
        )
    })?;

    let tier_rows = sqlx::query(
        "SELECT risk_tier, COUNT(*) AS cnt FROM orders GROUP BY risk_tier ORDER BY risk_tier",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|err| {
        (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({
                "error": format!("tier breakdown query failed: {}", err)
            })),
        )
    })?;

    let mut by_risk_tier = BTreeMap::new();
    for r in tier_rows {
        let tier: String = r.try_get("risk_tier").unwrap_or_else(|_| "UNKNOWN".to_string());
        let cnt: i64 = r.try_get("cnt").unwrap_or(0);
        by_risk_tier.insert(tier, cnt);
    }

    let summary = AnalyticsSummary {
        total_orders: totals_row.try_get("total_orders").unwrap_or(0),
        total_amount_cents: totals_row.try_get("total_amount_cents").unwrap_or(0),
        avg_risk_score: totals_row.try_get("avg_risk_score").unwrap_or(0.0),
        by_risk_tier,
    };

    let json_val = serde_json::to_value(&summary).unwrap_or_else(|_| serde_json::json!({}));

    if let Ok(mut conn) = state.redis_client.get_multiplexed_async_connection().await {
        if let Ok(serialized) = serde_json::to_string(&json_val) {
            let _: Result<(), _> = conn.set_ex(CACHE_KEY, serialized, 30).await;
        }
    }

    let mut headers = HeaderMap::new();
    headers.insert(
        header::HeaderName::from_static("x-cache"),
        HeaderValue::from_static("MISS"),
    );
    Ok((StatusCode::OK, headers, Json(json_val)))
}
