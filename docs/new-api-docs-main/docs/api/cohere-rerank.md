# Cohere 重排序格式（Rerank）

!!! warning "重要提示"
    cohere 的 Rerank 模型接口和 [Jina的Rerank模型接口格式](jinaai-rerank.md) 是一样的。

!!! info "官方文档"
    [Cohere Rerank](https://docs.cohere.com/reference/rerank)

## 📝 简介

给定查询和文本列表，重排序API将根据与查询的相关性对文本进行排序。每个文本都会被分配一个相关性分数，从而产生一个有序的数组结果。此功能特别适用于搜索和检索应用，可以优化文档的排序，帮助用户更快找到相关信息。

## 💡 请求示例

### 基础重排序请求 ✅

```bash
curl https://你的newapi服务器地址/v1/rerank \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "rerank-v3.5",
    "query": "什么是美国的首都？",
    "documents": [
      "内华达州的首府是卡森城。",
      "北马里亚纳群岛是太平洋上的一组岛屿，其首都是塞班岛。",
      "华盛顿特区（也称为华盛顿或特区，正式名称为哥伦比亚特区）是美国的首都。",
      "英语语法中的大写是在单词开头使用大写字母。英语用法与其他语言的大写不同。",
      "自美国成为一个国家之前，美国就存在死刑。截至2017年，在50个州中有30个州死刑合法。"
    ],
    "top_n": 3
  }'
```

**响应示例:**

```json
{
  "results": [
    {
      "index": 2,
      "relevance_score": 0.999071
    },
    {
      "index": 0,
      "relevance_score": 0.32713068
    },
    {
      "index": 1,
      "relevance_score": 0.1867867
    }
  ],
  "id": "07734bd2-2473-4f07-94e1-0d9f0e6843cf",
  "meta": {
    "api_version": {
      "version": "2",
      "is_experimental": false
    },
    "billed_units": {
      "search_units": 1
    }
  }
}
```

### 使用结构化数据 ✅

```bash
curl https://你的newapi服务器地址/v1/rerank \
  -H "Authorization: Bearer $NEWAPI_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "rerank-v3.5",
    "query": "寻找适合初学者的高性价比单反相机",
    "documents": [
      "型号: Canon EOS 800D\n价格: 4299元\n功能: 2410万像素, 光学取景器, Wi-Fi\n适合人群: 初学者, 爱好者",
      "型号: Nikon D3500\n价格: 3099元\n功能: 2416万像素, 光学取景器, 长达1550张的电池续航\n适合人群: 新手, 学生",
      "型号: Sony A7III\n价格: 12999元\n功能: 2420万像素, 全画幅, 4K视频\n适合人群: 专业摄影师, 视频创作者"
    ],
    "max_tokens_per_doc": 512
  }'
```

**响应示例:**

```json
{
  "results": [
    {
      "index": 1,
      "relevance_score": 0.918472
    },
    {
      "index": 0,
      "relevance_score": 0.854321
    },
    {
      "index": 2,
      "relevance_score": 0.423156
    }
  ],
  "id": "8f734bd2-2473-4f07-94e1-0d9f0e68ebfa",
  "meta": {
    "api_version": {
      "version": "2"
    },
    "billed_units": {
      "search_units": 1
    }
  }
}
```

## 📮 请求

### 端点

```
POST /v1/rerank
```

根据与查询的相关性对文本列表进行重新排序。

### 鉴权方法

在请求头中包含以下内容进行 API 密钥认证：

```
Authorization: Bearer $NEWAPI_API_KEY
```

其中 `$NEWAPI_API_KEY` 是您的 API 密钥。

### 请求头参数

#### `X-Client-Name`
- 类型：字符串
- 必需：否
- 说明：发起请求的项目名称。

### 请求体参数

#### `model`
- 类型：字符串
- 必需：是
- 说明：要使用的模型标识符，例如 rerank-v3.5。

#### `query`
- 类型：字符串
- 必需：是
- 说明：搜索查询文本。这是用户输入的问题或查询内容。

#### `documents`
- 类型：字符串数组
- 必需：是
- 说明：将与查询进行比较的文本列表。为获得最佳性能，建议单个请求中不要发送超过1,000个文档。
- 注意事项：
  - 长文档将自动截断为 max_tokens_per_doc 指定的值
  - 结构化数据应格式化为YAML字符串以获得最佳性能

#### `top_n`
- 类型：整数
- 必需：否
- 说明：限制返回的重排结果数量。如果不指定，将返回所有重排结果。

#### `max_tokens_per_doc`
- 类型：整数
- 必需：否
- 默认值：4096
- 说明：长文档将自动截断为指定的令牌数量。

## 📥 响应

### 成功响应

返回一个包含排序后文档列表的对象。

#### `results`
- 类型：对象数组
- 说明：排序后的文档列表，按相关性降序排列
- 属性：
  - `index`: 整数，对应于原始文档列表中文档的索引
  - `relevance_score`: 浮点数，相关性分数范围为[0, 1]。接近1的分数表示与查询高度相关，接近0的分数表示相关性较低

#### `id`
- 类型：字符串
- 说明：请求的唯一标识符

#### `meta`
- 类型：对象
- 说明：包含关于请求的元数据
- 属性：
  - `api_version`: 对象，包含API版本信息
    - `version`: 字符串，API版本号
    - `is_deprecated`: 布尔值，是否已弃用
    - `is_experimental`: 布尔值，是否为实验性功能
  - `billed_units`: 对象，包含计费信息
    - `search_units`: 浮点数，计费的搜索单位数
  - `tokens`: 对象，包含令牌使用统计
    - `input_tokens`: 浮点数，作为模型输入的令牌数
    - `output_tokens`: 浮点数，模型产生的令牌数

#### `warnings`
- 类型：字符串数组
- 必需：否
- 说明：API返回的警告信息

### 错误响应

当请求出现问题时，API可能返回以下HTTP状态码及相应错误：

- `400 Bad Request`: 请求格式或参数错误
- `401 Unauthorized`: 未提供有效的API密钥
- `403 Forbidden`: 没有权限访问此资源
- `404 Not Found`: 请求的资源不存在
- `422 Unprocessable Entity`: 请求格式正确但包含语义错误
- `429 Too Many Requests`: 请求频率超过限制
- `500 Internal Server Error`: 服务器内部错误
- `503 Service Unavailable`: 服务暂时不可用

## 🌟 最佳实践

### 文档准备建议

1. **文档长度**：每个文档保持简洁明了，避免过长。长文档会被自动截断。
   
2. **结构化数据**：将结构化数据格式化为YAML字符串，以获得最佳性能。例如：
   ```yaml
   title: 产品名称
   price: 9999元
   features:
     - 特性1
     - 特性2
   ```

3. **文档数量**：单次请求中不要超过1,000个文档，以获得最佳性能。

### 查询优化

1. **明确具体**：制定明确、具体的查询，以获得更准确的排序结果。

2. **避免模糊查询**：尽量避免过于模糊或通用的查询词，这可能导致相关性分数差异不明显。

### 理解相关性分数

相关性分数是归一化到[0, 1]范围内的值：

- 接近1的分数表示与查询高度相关

- 接近0的分数表示相关性低

注意：不能简单地认为分数0.9的文档比分数0.45的文档相关性高2倍。相关性分数是一个相对指标，用于排序，而非绝对比较。
