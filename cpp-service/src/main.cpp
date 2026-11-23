#include "httplib.h"

#include <atomic>
#include <chrono>
#include <csignal>
#include <cstdlib>
#include <iostream>
#include <string>
#include <unistd.h>

namespace {
httplib::Server *g_server = nullptr;
std::atomic<bool> g_shutting_down{false};

void handle_signal(int signum) {
  g_shutting_down.store(true);
  if (g_server != nullptr) {
    g_server->stop();
  }
  (void)signum;
}

std::string get_env_or(const char *key, const std::string &fallback) {
  const char *val = std::getenv(key);
  return (val != nullptr && val[0] != '\0') ? std::string(val) : fallback;
}
} // namespace

int main() {
  const std::string host = get_env_or("APP_HOST", "127.0.0.1");
  const int port = std::stoi(get_env_or("APP_PORT", "8083"));
  const std::string env = get_env_or("APP_ENV", "host-native");
  const auto start_time = std::chrono::steady_clock::now();

  httplib::Server svr;
  g_server = &svr;

  std::signal(SIGINT, handle_signal);
  std::signal(SIGTERM, handle_signal);

  svr.Get("/healthz", [](const httplib::Request &, httplib::Response &res) {
    res.set_content(R"({"status":"ok","service":"cpp-api"})",
                    "application/json");
  });

  svr.Get("/readyz", [](const httplib::Request &, httplib::Response &res) {
    if (g_shutting_down.load()) {
      res.status = 503;
      res.set_content(R"({"status":"shutting_down","service":"cpp-api"})",
                      "application/json");
      return;
    }
    res.set_content(R"({"status":"ready","service":"cpp-api"})",
                    "application/json");
  });

  svr.Get("/api/v1/info", [&env, start_time](const httplib::Request &,
                                             httplib::Response &res) {
    const auto uptime = std::chrono::duration_cast<std::chrono::seconds>(
                            std::chrono::steady_clock::now() - start_time)
                            .count();
    const std::string body =
        std::string(R"({"service":"cpp-api","version":"1.0.0","env":")") + env +
        R"(","cpp_standard":)" + std::to_string(__cplusplus) + R"(,"pid":)" +
        std::to_string(getpid()) + R"(,"uptime_seconds":)" +
        std::to_string(uptime) + "}";
    res.set_content(body, "application/json");
  });

  std::cout << "cpp-api listening on " << host << ":" << port << " (env=" << env
            << ")" << std::endl;
  if (!svr.listen(host, port)) {
    std::cerr << "Failed to bind " << host << ":" << port << std::endl;
    return 1;
  }

  std::cout << "cpp-api stopped cleanly" << std::endl;
  return 0;
}
