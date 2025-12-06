# OpenAI 音频格式

!!! info "官方文档"
    [OpenAI Audio](https://platform.openai.com/docs/api-reference/audio)

## 📝 简介

OpenAI 音频 API 提供了三个主要功能:

1. 文本转语音(TTS) - 将文本转换为自然的语音
2. 语音转文本(STT) - 将音频转录为文本
3. 音频翻译 - 将非英语音频翻译成英语文本

## 💡 请求示例

### 文本转语音 ✅

```bash
curl https://你的newapi服务器地址/v1/audio/speech \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "tts-1",
    "input": "你好,世界!",
    "voice": "alloy"
  }' \
  --output speech.mp3
```

### 语音转文本 ✅

```bash
curl https://你的newapi服务器地址/v1/audio/transcriptions \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -H "Content-Type: multipart/form-data" \
  -F file="@/path/to/file/audio.mp3" \
  -F model="whisper-1"
```

**响应示例:**

```json
{
  "text": "你好,世界!"
}
```

### 音频翻译 ✅

```bash
curl https://你的newapi服务器地址/v1/audio/translations \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -H "Content-Type: multipart/form-data" \
  -F file="@/path/to/file/chinese.mp3" \
  -F model="whisper-1"
```

**响应示例:**

```json
{
  "text": "Hello, world!"
}
```

## 📮 请求

### 端点

#### 文本转语音
```
POST /v1/audio/speech
```

将文本转换为语音。

#### 语音转文本
```
POST /v1/audio/transcriptions
```

将音频转录为输入语言的文本。

#### 音频翻译
```
POST /v1/audio/translations
```

将音频翻译为英语文本。

### 鉴权方法

在请求头中包含以下内容进行 API 密钥认证：

```
Authorization: Bearer $NEWAPI_API_KEY
```

其中 `$NEWAPI_API_KEY` 是您的 API 密钥。

### 请求体参数

#### 文本转语音

##### `model`
- 类型：字符串
- 必需：是
- 可选值：tts-1, tts-1-hd
- 说明：要使用的 TTS 模型

##### `input`
- 类型：字符串  
- 必需：是
- 最大长度：4096 字符
- 说明：要转换为语音的文本

##### `voice`
- 类型：字符串
- 必需：是
- 可选值：alloy, echo, fable, onyx, nova, shimmer
- 说明：生成语音时使用的声音

##### `response_format`
- 类型：字符串
- 必需：否
- 默认值：mp3
- 可选值：mp3, opus, aac, flac, wav, pcm
- 说明：音频输出格式

##### `speed`
- 类型：数字
- 必需：否
- 默认值：1.0
- 范围：0.25 - 4.0
- 说明：生成语音的速度

#### 语音转文本

##### `file`
- 类型：文件
- 必需：是
- 支持格式：flac, mp3, mp4, mpeg, mpga, m4a, ogg, wav, webm
- 说明：要转录的音频文件

##### `model`
- 类型：字符串
- 必需：是
- 当前仅支持：whisper-1
- 说明：要使用的模型 ID

##### `language`
- 类型：字符串
- 必需：否
- 格式：ISO-639-1 (如 "en")
- 说明：音频的语言,提供可提高准确性

##### `prompt`
- 类型：字符串
- 必需：否
- 说明：用于指导模型风格或继续前一段音频的文本

##### `response_format`
- 类型：字符串
- 必需：否
- 默认值：json
- 可选值：json, text, srt, verbose_json, vtt
- 说明：输出格式

##### `temperature`
- 类型：数字
- 必需：否
- 默认值：0
- 范围：0 - 1
- 说明：采样温度,较高的值使输出更随机

##### `timestamp_granularities`
- 类型：数组
- 必需：否
- 默认值：segment
- 可选值：word, segment
- 说明：转录的时间戳粒度

#### 音频翻译

##### `file`
- 类型：文件
- 必需：是
- 支持格式：flac, mp3, mp4, mpeg, mpga, m4a, ogg, wav, webm
- 说明：要翻译的音频文件

##### `model`
- 类型：字符串
- 必需：是
- 当前仅支持：whisper-1
- 说明：要使用的模型 ID

##### `prompt`
- 类型：字符串
- 必需：否
- 说明：用于指导模型风格的英文文本

##### `response_format`
- 类型：字符串
- 必需：否
- 默认值：json
- 可选值：json, text, srt, verbose_json, vtt
- 说明：输出格式

##### `temperature`
- 类型：数字
- 必需：否
- 默认值：0
- 范围：0 - 1
- 说明：采样温度,较高的值使输出更随机

## 📥 响应

### 成功响应

#### 文本转语音

返回二进制音频文件内容。

#### 语音转文本

##### 基础 JSON 格式

```json
{
  "text": "转录的文本内容"
}
```

##### 详细 JSON 格式

```json
{
  "task": "transcribe",
  "language": "english",
  "duration": 8.47,
  "text": "完整的转录文本",
  "segments": [
    {
      "id": 0,
      "seek": 0,
      "start": 0.0,
      "end": 3.32,
      "text": "分段的转录文本",
      "tokens": [50364, 440, 7534],
      "temperature": 0.0,
      "avg_logprob": -0.286,
      "compression_ratio": 1.236,
      "no_speech_prob": 0.009
    }
  ]
}
```

#### 音频翻译

```json
{
  "text": "翻译后的英文文本"
}
```

### 错误响应

当请求出现问题时，API 将返回一个错误响应对象，HTTP 状态码在 4XX-5XX 范围内。

#### 常见错误状态码

- `400 Bad Request`: 请求参数无效
- `401 Unauthorized`: API 密钥无效或未提供
- `429 Too Many Requests`: 超出 API 调用限制
- `500 Internal Server Error`: 服务器内部错误

错误响应示例:

```json
{
  "error": {
    "message": "文件格式不支持",
    "type": "invalid_request_error",
    "param": "file",
    "code": "invalid_file_format"
  }
}
``` 