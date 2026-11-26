package com.aditya.polyglot;

import com.fasterxml.jackson.databind.ObjectMapper;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.data.redis.connection.RedisConnection;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.jdbc.core.JdbcTemplate;
import org.springframework.web.bind.annotation.*;

import java.math.BigDecimal;
import java.math.RoundingMode;
import java.time.Duration;
import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.TreeMap;

@RestController
@SuppressWarnings({"rawtypes", "unchecked"})
public class ApiController {

    private static final String CACHE_KEY = "analytics:summary";

    private final JdbcTemplate jdbcTemplate;
    private final StringRedisTemplate redisTemplate;
    private final ObjectMapper objectMapper;
    private final String appEnv;
    private final Instant startedAt = Instant.now();

    public ApiController(
            JdbcTemplate jdbcTemplate,
            StringRedisTemplate redisTemplate,
            ObjectMapper objectMapper,
            @Value("${app.env:host-native}") String appEnv) {
        this.jdbcTemplate = jdbcTemplate;
        this.redisTemplate = redisTemplate;
        this.objectMapper = objectMapper;
        this.appEnv = appEnv;
    }

    public record ScoreRequest(String customer_id, long amount_cents) {}

    public record ScoreResponse(
            String customer_id,
            int recent_orders,
            double risk_score,
            String risk_tier) {}

    @GetMapping("/healthz")
    public Map healthz() {
        Map body = new LinkedHashMap();
        body.put("status", "ok");
        body.put("service", "java-api");
        body.put("env", appEnv);
        body.put("uptime_seconds", Duration.between(startedAt, Instant.now()).toSeconds());
        return body;
    }

    @GetMapping("/readyz")
    public ResponseEntity readyz() {
        try {
            Integer one = jdbcTemplate.queryForObject("SELECT 1", Integer.class);
            if (one == null || one != 1) {
                throw new IllegalStateException("MySQL SELECT 1 returned unexpected value");
            }
            try (RedisConnection conn = redisTemplate.getRequiredConnectionFactory().getConnection()) {
                String pong = conn.ping();
                if (!"PONG".equalsIgnoreCase(pong)) {
                    throw new IllegalStateException("Redis PING failed");
                }
            }
            return ResponseEntity.ok(Map.of("status", "ready", "service", "java-api"));
        } catch (Exception ex) {
            return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
                    .body(Map.of("status", "not_ready", "error", String.valueOf(ex.getMessage())));
        }
    }

    @PostMapping("/internal/v1/score")
    public ResponseEntity computeRiskScore(@RequestBody ScoreRequest payload) {
        if (payload == null || payload.customer_id() == null || payload.customer_id().isBlank()
                || payload.amount_cents() <= 0) {
            return ResponseEntity.badRequest()
                    .body(Map.of("error", "customer_id and positive amount_cents are required"));
        }

        int recentOrders;
        try {
            Integer count = jdbcTemplate.queryForObject(
                    """
                    SELECT COUNT(*)
                    FROM orders
                    WHERE customer_id = ?
                      AND created_at >= DATE_SUB(NOW(6), INTERVAL 24 HOUR)
                    """,
                    Integer.class,
                    payload.customer_id());
            recentOrders = (count != null) ? count : 0;
        } catch (Exception ex) {
            return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
                    .body(Map.of("error", "database query failed: " + ex.getMessage()));
        }

        double amountDollars = payload.amount_cents() / 100.0;
        double rawScore = (amountDollars / 100.0) + (recentOrders * 15.0);
        double score = Math.min(99.99,
                BigDecimal.valueOf(rawScore).setScale(2, RoundingMode.HALF_UP).doubleValue());

        String tier;
        if (score >= 70.0) {
            tier = "HIGH";
        } else if (score >= 35.0) {
            tier = "MEDIUM";
        } else {
            tier = "LOW";
        }

        return ResponseEntity.ok(new ScoreResponse(
                payload.customer_id(),
                recentOrders,
                score,
                tier));
    }

    @GetMapping("/api/v1/analytics/summary")
    public ResponseEntity analyticsSummary() {
        try {
            String cached = redisTemplate.opsForValue().get(CACHE_KEY);
            if (cached != null && !cached.isBlank()) {
                Map parsed = objectMapper.readValue(cached, LinkedHashMap.class);
                HttpHeaders headers = new HttpHeaders();
                headers.add("X-Cache", "HIT");
                return new ResponseEntity(parsed, headers, HttpStatus.OK);
            }
        } catch (Exception ignored) {
        }

        Map summary = new LinkedHashMap();
        try {
            Map totals = jdbcTemplate.queryForMap(
                    """
                    SELECT
                        COUNT(*) AS total_orders,
                        COALESCE(SUM(amount_cents), 0) AS total_amount_cents,
                        COALESCE(ROUND(AVG(risk_score), 2), 0.00) AS avg_risk_score
                    FROM orders
                    """);

            Map byTier = new TreeMap();
            jdbcTemplate.query(
                    """
                    SELECT risk_tier, COUNT(*) AS cnt
                    FROM orders
                    GROUP BY risk_tier
                    ORDER BY risk_tier
                    """,
                    rs -> {
                        byTier.put(rs.getString("risk_tier"), rs.getLong("cnt"));
                    });

            summary.put("total_orders", ((Number) totals.get("total_orders")).longValue());
            summary.put("total_amount_cents", ((Number) totals.get("total_amount_cents")).longValue());
            summary.put("avg_risk_score", ((Number) totals.get("avg_risk_score")).doubleValue());
            summary.put("by_risk_tier", byTier);
        } catch (Exception ex) {
            return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
                    .body(Map.of("error", "analytics query failed: " + ex.getMessage()));
        }

        try {
            redisTemplate.opsForValue().set(
                    CACHE_KEY, objectMapper.writeValueAsString(summary), Duration.ofSeconds(30));
        } catch (Exception ignored) {
        }

        HttpHeaders headers = new HttpHeaders();
        headers.add("X-Cache", "MISS");
        return new ResponseEntity(summary, headers, HttpStatus.OK);
    }
}
