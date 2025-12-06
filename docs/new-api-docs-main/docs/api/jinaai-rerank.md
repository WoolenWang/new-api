# Jina AI 重排序格式（Rerank）

!!! info "官方文档"
    [Jina AI Rerank](https://jina.ai/reranker)

!!! note "标准格式"
    在New API中，Jina AI的rerank格式被采用为标准格式。所有其他供应商（如Xinference、Cohere等）的rerank响应都会被格式化为Jina AI的格式，以提供统一的开发体验。

## 📝 简介

Jina AI Rerank 是一个强大的文本重排序模型，可以根据查询对文档列表进行相关性排序。该模型支持多语言，可以处理不同语言的文本内容，并为每个文档分配相关性分数。

## 💡 请求示例

### 基础重排序请求 ✅

```bash
curl https://你的newapi服务器地址/v1/rerank \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -d '{
    "model": "jina-reranker-v2-base-multilingual",
    "query": "Organic skincare products for sensitive skin",
    "top_n": 3,
    "documents": [
      "Organic skincare for sensitive skin with aloe vera and chamomile...",
      "New makeup trends focus on bold colors and innovative techniques...",
      "Bio-Hautpflege für empfindliche Haut mit Aloe Vera und Kamille..."
    ]
  }'
```

**响应示例:**

```json
{
  "results": [
    {
      "document": {
        "text": "Organic skincare for sensitive skin with aloe vera and chamomile..."
      },
      "index": 0,
      "relevance_score": 0.8783142566680908
    },
    {
      "document": {
        "text": "Bio-Hautpflege für empfindliche Haut mit Aloe Vera und Kamille..."
      },
      "index": 2,
      "relevance_score": 0.7624675869941711
    }
  ],
  "usage": {
    "prompt_tokens": 815,
    "completion_tokens": 0,
    "total_tokens": 815
  }
}
```

## 📮 请求

### 端点

```
POST /v1/rerank
```

### 鉴权方法

在请求头中包含以下内容进行 API 密钥认证：

```
Authorization: Bearer $NEWAPI_API_KEY
```

其中 `$NEWAPI_API_KEY` 是您的 API 密钥。

### 请求体参数

#### `model`
- 类型：字符串
- 必需：否
- 默认值：jina-reranker-v2-base-multilingual
- 说明：要使用的重排序模型

#### `query`
- 类型：字符串
- 必需：是
- 说明：用于对文档进行相关性排序的查询文本

#### `top_n`
- 类型：整数
- 必需：否
- 默认值：无限制
- 说明：返回排序后的前 N 个文档

#### `documents`
- 类型：字符串数组
- 必需：是
- 说明：要进行重排序的文档列表
- 限制：每个文档的长度不应超过模型的最大token限制

## 📥 响应

### 成功响应

#### `results`
- 类型：数组
- 说明：重排序后的文档列表
- 属性：
  - `document`: 包含文档文本的对象
  - `index`: 文档在原始列表中的索引
  - `relevance_score`: 相关性分数(0-1之间)

#### `usage`
- 类型：对象
- 说明：token 使用统计
- 属性：
  - `prompt_tokens`: 提示使用的 token 数
  - `completion_tokens`: 补全使用的 token 数
  - `total_tokens`: 总 token 数
  - `prompt_tokens_details`: 提示 token 详细信息
    - `cached_tokens`: 缓存的 token 数
    - `audio_tokens`: 音频 token 数
  - `completion_tokens_details`: 补全 token 详细信息
    - `reasoning_tokens`: 推理 token 数
    - `audio_tokens`: 音频 token 数
    - `accepted_prediction_tokens`: 接受的预测 token 数
    - `rejected_prediction_tokens`: 拒绝的预测 token 数

### 错误响应

当请求出现问题时，API 将返回错误响应：

- `400 Bad Request`: 请求参数无效
- `401 Unauthorized`: API 密钥无效或未提供
- `429 Too Many Requests`: 请求频率超限
- `500 Internal Server Error`: 服务器内部错误

## 💡 最佳实践

### 查询优化建议

1. 使用清晰具体的查询文本
2. 避免过于宽泛或模糊的查询
3. 确保查询与文档使用相同的语言风格

### 文档处理建议

1. 保持文档长度适中，不要超过模型限制
2. 确保文档内容完整且有意义
3. 可以包含多语言文档，模型支持跨语言匹配

### 性能优化

1. 合理设置 top_n 参数以减少不必要的计算
2. 对于大量文档，考虑分批处理
3. 可以缓存常用查询的结果

### 多语言支持

该模型支持多种语言的文档重排序，包括但不限于：

- 英语
- 中文
- 德语
- 西班牙语
- 日语
- 法语

无需指定语言参数，模型会自动识别和处理不同语言的内容。
