cmake_minimum_required(VERSION 3.20)
project(cpp_api VERSION 1.0.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD 20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

find_package(Threads REQUIRED)

add_executable(cpp-api src/main.cpp)
target_include_directories(cpp-api SYSTEM PRIVATE "${CMAKE_CURRENT_SOURCE_DIR}/third_party")
target_compile_options(cpp-api PRIVATE -Wall -Wextra -Wpedantic)
target_link_libraries(cpp-api PRIVATE Threads::Threads)
