@echo off
REM 套餐优先级与Fallback测试执行脚本 (Windows)

echo ==================================
echo NewAPI 套餐优先级与Fallback测试
echo ==================================
echo.

cd /d %~dp0

echo [1/4] 清理之前的测试输出...
if exist coverage.out del coverage.out
if exist coverage.html del coverage.html
if exist test-output.log del test-output.log

echo [2/4] 运行所有P0级别测试...
echo   - PF-01: 单套餐未超限
echo   - PF-02: 单套餐超限-允许Fallback
echo   - PF-03: 单套餐超限-禁止Fallback
echo   - PF-04: 多套餐优先级降级
echo   - PF-06: 所有套餐超限-Fallback
echo   - PF-07: 所有套餐超限-无Fallback
echo   - PF-09: 多窗口任一超限即失败
echo.

go test -v -run "TestPF0[1-4,6-7,9]" > test-output.log 2>&1
if errorlevel 1 (
    echo ❌ P0测试存在失败
    type test-output.log
    goto :error
)

echo.
echo [3/4] 运行P1级别测试...
echo   - PF-05: 优先级相同按ID排序
echo   - PF-08: 月度总限额优先检查
echo.

go test -v -run "TestPF0[5,8]" >> test-output.log 2>&1

echo.
echo [4/4] 生成覆盖率报告...
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html
echo   ✓ 覆盖率报告已生成: coverage.html

echo.
echo ==================================
echo 测试完成！
echo ==================================
echo.
echo 查看详细日志: type test-output.log
echo 查看覆盖率: start coverage.html
echo.

findstr /C:"PASS" test-output.log > nul
if errorlevel 1 (
    echo ❌ 存在失败的测试用例，请检查日志
    goto :error
) else (
    echo ✅ 所有测试用例通过！
    goto :success
)

:error
exit /b 1

:success
exit /b 0
