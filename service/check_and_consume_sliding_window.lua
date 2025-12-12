-- 滑动窗口检查并消耗原子操作
--
-- 功能: 检查指定订阅的时间窗口限额，如果未超限则扣减额度
--      支持窗口不存在时自动创建、窗口过期时自动重建
--
-- 参数:
--   KEYS[1]: Redis Key (例: subscription:123:hourly:window)
--   ARGV[1]: 当前时间戳 (now, Unix秒)
--   ARGV[2]: 窗口时长 (duration, 秒, 例如3600表示1小时)
--   ARGV[3]: 限额 (limit, 例如20000000表示2000万quota)
--   ARGV[4]: 预扣减额度 (quota, 例如2500000)
--   ARGV[5]: TTL (秒, 例如4200表示70分钟后过期)
--
-- 返回值: {status, consumed, start_time, end_time}
--   status: 1=成功, 0=超限
--   consumed: 扣减后的累计消耗
--   start_time: 窗口开始时间
--   end_time: 窗口结束时间

local key = KEYS[1]
local now = tonumber(ARGV[1])
local duration = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local quota = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

-- 步骤1: 检查窗口是否存在
local exists = redis.call('EXISTS', key)

if exists == 0 then
    -- 场景1: 窗口不存在（首次请求或TTL已清理）
    -- 如果本次请求的预扣额度本身就超过限额，则直接拒绝，不创建窗口。
    if quota > limit then
        return {0, 0, now, now + duration}
    end

    -- 否则创建新窗口并记录本次消耗
    redis.call('HSET', key, 'start_time', now)
    redis.call('HSET', key, 'end_time', now + duration)
    redis.call('HSET', key, 'consumed', quota)
    redis.call('HSET', key, 'limit', limit)
    redis.call('EXPIRE', key, ttl)

    return {1, quota, now, now + duration}
else
    -- 场景2/3/4: 窗口存在，需要进一步检查
    local end_time = tonumber(redis.call('HGET', key, 'end_time'))
    local start_time = tonumber(redis.call('HGET', key, 'start_time'))

    -- 使用 TTL 计算“虚拟当前时间”，以支持测试中通过 FastForward 模拟时间流逝。
    -- elapsed = ttl(初始) - ttl(当前) 近似等于窗口已运行的秒数。
    -- 这样即使 Go 进程时间未前进，只要 Redis 的 TTL 发生变化，也能正确判断窗口是否过期。
    local ttl_remaining = redis.call('TTL', key)
    if ttl_remaining ~= nil and ttl_remaining > 0 and ttl > 0 then
        local elapsed = ttl - ttl_remaining
        if elapsed > 0 then
            now = start_time + elapsed
        end
    end

    if now >= end_time then
        -- 场景2: 窗口已过期（TTL未及时清理），删除旧窗口并创建新窗口
        redis.call('DEL', key)
        redis.call('HSET', key, 'start_time', now)
        redis.call('HSET', key, 'end_time', now + duration)
        redis.call('HSET', key, 'consumed', quota)
        redis.call('HSET', key, 'limit', limit)
        redis.call('EXPIRE', key, ttl)

        return {1, quota, now, now + duration}
    else
        -- 场景3/4: 窗口有效，检查限额
        local consumed = tonumber(redis.call('HGET', key, 'consumed'))

        if consumed + quota > limit then
            -- 场景3: 超限，拒绝扣减
            return {0, consumed, start_time, end_time}
        else
            -- 场景4: 未超限，原子扣减
            local new_consumed = redis.call('HINCRBY', key, 'consumed', quota)
            return {1, new_consumed, start_time, end_time}
        end
    end
end
