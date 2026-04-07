# Toktik Options Platform Product Design

## 1. Product Positioning

### 1.1 Slogan

让期权投资变得简单。

### 1.2 Product Definition

Toktik 不是单纯的数据终端，也不是单纯的回测工具，而是一个面向期权交易决策的智能平台。平台基于多市场期权链、波动率特征、流动性特征、事件窗口、预置策略模板和回测引擎，输出可执行的策略建议、卖方机会筛选和风险解释。

平台目标是把“看不懂期权链”和“知道策略但不会选时机”这两个核心问题产品化解决。

平台在定位上应明确是“教育 + 风险管理 + 策略推荐”工具，而不是代客决策或收益承诺工具。所有推荐都需要同时给出适用原因、主要风险、最大亏损和管理建议。

### 1.3 Target Users

- 成长中的新手用户：刚理解基础期权概念，准备从学习走向模拟盘或小仓位实盘，需要低复杂度、强解释和硬护栏。
- 进阶交易者：理解常见策略结构，但不确定“当前市场环境适合什么策略”，需要系统化择时与多策略比较。
- 专业研究与量化用户：关注特征面板、历史验证、Greeks 暴露与策略回测，需要更高自由度的研究入口。

## 2. Problem Statement

当前用户在做期权交易决策时，通常面临以下问题：

- 不知道先选哪个标的，只能在大量 ETF、指数、个股和加密品种中凭经验翻找。
- 知道一些经典策略，但不知道当前市场环境更适合买方还是卖方。
- 看见高 IV 或大波动时，不知道应该卖波动率、做方向价差，还是等待。
- 很难把趋势、IV 分位、期限结构、偏度、流动性、事件日程放到同一套决策上下文里。
- 回测、特征和实时筛选之间往往割裂，无法形成稳定可复用的决策流水线。

## 3. Product Goals

### 3.1 Business Goals

- 建立一个可扩展的期权推荐平台，先覆盖美股和 Crypto 重点标的。
- 把现有数据基础设施升级成用户可感知的“机会发现 + 策略推荐 + 风险解释”产品。
- 形成可持续扩展的新市场接入框架，后续支持更多 ETF、指数、商品和全球核心资产。

### 3.2 User Goals

- 用户能在 3 分钟内知道“今天值得看哪些期权机会”。
- 用户能理解为什么推荐该策略，而不是只看到一个黑盒结果。
- 用户能比较多个候选策略的胜率、盈亏比、Greeks 暴露和流动性质量。
- 卖方用户能快速找到在 Delta、Gamma、Vega、Theta 四项中至少具备 3 项优势的候选机会。
- 新手用户能先看懂“你赚的是什么、你最怕什么、该怎么进、该怎么管”，再决定是否继续研究具体合约。

### 3.3 Non-Goals

- 首期不做自动下单和券商执行。
- 首期不做开放式用户自定义 Agent 编排平台。
- 首期不承诺全市场全品种覆盖，而是优先覆盖可稳定接入的数据源。
- 首期不鼓励高复杂度、超短周期或高爆仓风险策略作为默认推荐入口。

## 4. Scope

### 4.1 Initial Coverage

- 美股 ETF 大类资产代表品种。
- 核心指数与指数期权：纳指、标普及其代理产品。
- 美股核心科技股：AAPL、MSFT、GOOGL、AMZN、NVDA、META、TSLA。
- Crypto：BTC、ETH、BNB、SOL。
- 后续扩展：黄金、白银、沪深 300 及其他全球核心品种。

### 4.2 Supported Decision Types

- 交易型推荐：为特定标的推荐适合当前环境的方向性或波动率策略。
- 卖方机会推荐：优先筛选高胜率、高时间价值收割质量、Greeks 结构占优的组合。
- 研究型分析：为量化开发者输出市场上下文、特征快照和历史验证入口。

## 5. Existing Platform Capabilities

本节只列当前仓库中已经具备或明确实现的能力，作为产品设计的真实基础。

### 5.1 Data and Infra

- 多市场统一数据平台：Crypto options、US options、US stocks。
- ClickHouse 1m 基础表和多周期 K-line 预聚合视图。
- 统一市场 API 和基础设施可观测接口。
- 特征存储与回填能力，包括 volatility、term structure、skew、liquidity、daily panel、event window。

### 5.2 Current Query and Analysis Surface

- 标的与合约查询：symbols、chain、bars、greeks。
- 市场能力发现：markets、datasets、freshness。
- 特征分析：
  - volatility snapshot/history
  - term-structure snapshot
  - skew snapshot
  - liquidity snapshot/history
  - event-window snapshot/history
  - daily-feature-panel

### 5.3 Strategy and Evaluation Foundations

- 事件驱动回测引擎。
- 多资产、多腿期权策略支持。
- 预置策略目录，包括 bull put spread、bear call spread、covered call、forum-short-put、lvol-scalper 等。
- 报表输出能力，包括收益曲线、回撤和交易记录。

### 5.4 Important Constraint

当前平台的强项是“数据、特征、回测和 API 基础设施”；当前缺失的是面向终端用户的“推荐层、解释层、工作流层和统一产品交互层”。本设计文档的核心就是补齐这一层。

## 6. Expected Product Functions

### 6.1 Opportunity Discovery

平台需要直接告诉用户：

- 今天有哪些标的值得看。
- 每个标的当前处于什么市场状态。
- 当前更适合买方策略还是卖方策略。
- 最优先关注哪个期限、哪类行权价带。

### 6.2 Strategy Recommendation

针对每个 underlying，平台需要输出：

- 推荐策略类型。
- 推荐理由。
- 适用市场背景。
- 候选合约或候选价差结构。
- 预估胜率、盈亏比、最大风险、Greeks 暴露。
- 流动性和可执行性评分。

### 6.3 Seller Recommendation

平台需要单独提供“卖方看板”，优先输出：

- 高 IV 或波动率扭曲的卖方机会。
- 具有 Theta 收益优势且 Vega 回归逻辑成立的结构。
- 在 Delta、Gamma、Vega、Theta 四项中至少 3 项占优的候选策略。
- DTE 建议区间和原因。

### 6.4 Explainability

每条推荐必须回答三个问题：

- 为什么是这个标的。
- 为什么是这个策略，而不是其他策略。
- 这笔交易最主要的风险来自哪里。

### 6.5 Research-to-Execution Workflow

用户应当能从同一个推荐结果进入：

- 当前特征快照。
- 历史特征走势。
- 相关策略历史回测。
- 备选策略对比。

### 6.6 Beginner Learning and Safety Workflow

针对期权新手，平台需要提供一条“先理解，再交易”的默认路径：

- 先显示标的和环境，而不是直接显示复杂 Greeks 表。
- 先显示策略卡片和风险结构，再显示具体合约。
- 默认推荐有限风险、结构清晰、容易解释的策略。
- 对每条推荐强制显示最大亏损、保证金/资金占用、是否可能提前行权、是否跨事件窗口。
- 支持从推荐卡片一键跳转到策略知识卡片和术语解释。

## 7. Core Product Concept

### 7.1 Product Thesis

平台的核心价值不是“展示所有数据”，而是把数据转化为可执行决策：

1. 识别市场环境。
2. 识别期权曲面和流动性特征。
3. 匹配最适合的策略模板。
4. 评估风险收益与 Greeks 结构。
5. 生成可解释推荐。

### 7.2 Decision Engine Layers

产品决策引擎分为四层：

- Context Layer：市场趋势、波动率状态、期限结构、偏度、事件窗口、流动性。
- Strategy Mapping Layer：把市场上下文映射到经典策略模板。
- Validation Layer：使用回测、风控阈值和可执行性过滤候选策略。
- Explanation Layer：输出人类可读的推荐理由和风险说明。

## 8. User Experience Design

### 8.1 Primary User Flow

1. 用户进入平台首页。
2. 首页显示今日重点标的和市场环境概览。
3. 用户点击某个标的，进入 underlying intelligence 页面。
4. 页面展示当前上下文、推荐策略、卖方机会、历史验证和备选方案。
5. 用户进一步查看具体价差结构、关键 Greeks 和盈亏图。

### 8.2 Product Surfaces

#### A. Home: Market Opportunity Dashboard

目标：先告诉用户看哪里。

展示内容：

- 今日重点标的榜单。
- 按市场分类的机会列表：US ETF、US Tech、Crypto。
- 市场 regime 标签，例如“强趋势低 IV”“恐慌高 IV”“低波待突破”。
- 推荐动作标签，例如“优先买方”“优先卖方”“观望”。

#### B. Underlying Intelligence Page

目标：给单一标的完整决策上下文。

展示内容：

- 价格趋势摘要。
- IV 绝对水平、IV percentile、IV rank。
- ATM term structure、put-call skew。
- 流动性质量、OI、relative spread。
- 临近财报、假日、早收盘等事件上下文。
- 该标的下的推荐策略列表。

#### C. Strategy Recommendation Card

每张推荐卡需要包含：

- Strategy name。
- Strategy intent，例如 bullish / bearish / neutral / long vol / short vol / income。
- 推荐分数。
- 信心等级。
- 胜率估计。
- 盈亏比估计。
- 最大亏损和主要风险来源。
- 四项 Greeks 优势摘要。
- 推荐 DTE 和 strike zone。

#### D. Seller Board

目标：面向卖方用户快速排序。

排序维度：

- Theta quality。
- Vega mean-reversion edge。
- Delta safety buffer。
- Gamma risk control。
- Liquidity quality。
- Event risk cleanliness。

#### E. Quant Research View

目标：面向进阶用户和量化开发者。

展示内容：

- 原始 feature panel。
- 历史 regime 变化。
- 回测入口。
- 策略参数对比。

#### F. Beginner Mode

目标：把默认信息密度降低，先帮助用户理解结构。

展示内容：

- 今日只看 3 个最清晰机会。
- 每条推荐只显示四句法摘要、最大亏损和推荐原因。
- 风险标签，例如“有限风险”“跨事件窗口”“流动性一般”。
- 术语提示，例如 Delta、Theta、IV percentile 的一句话解释。

#### G. Strategy Library

目标：把推荐系统和知识资产连起来，降低新手理解门槛。

展示内容：

- 策略难度标签，例如新手 / 进阶 / 高级。
- 环境匹配说明：什么市场环境适合该策略。
- Greeks 画像：主要赚什么，主要怕什么。
- 典型参数范围：常见 DTE、Delta、宽度、止盈止损方式。
- 禁忌列表：不适合在哪些环境使用。

#### H. AI Copilot

目标：让用户用自然语言追问推荐理由和风险。

典型问题：

- “现在 SPY 的 IV 算高吗？”
- “为什么这里推荐 bull put spread 而不是 covered call？”
- “这条建议最怕什么？”
- “如果我是新手，只看有限风险策略，应该过滤成什么结果？”

## 9. Recommendation System Design

### 9.1 Context Inputs

推荐引擎首期直接复用已有数据和 API：

- Price trend and realized volatility。
- Current IV、IV percentile、IV rank。
- ATM IV term structure。
- Put-call skew。
- Liquidity metrics：relative spread、open interest、activity ratio、tradability ratio。
- Event window：holiday proximity、early close；后续扩展 earnings、macro news。
- Chain-level greeks and contract metadata。

### 9.1.1 Candidate Pool and Liquidity Gating

外部设计文档里“先筛流动性再做推荐”的原则应当保留，并落为产品级硬规则。

首期推荐池应优先包含：

- 流动性最好的美股 ETF、指数相关品种、核心科技股和 Crypto 重点资产。
- 对新手优先展示信息透明、链条连续、点差较稳的标的。

建议的首期降级或过滤条件：

- 相对点差过大。
- OI、成交量或活跃合约数过低。
- 关键 Greeks 或链数据缺失。
- 事件风险太近且当前解释能力不足。

### 9.2 Regime Classification

系统先把标的映射到市场状态，再做策略匹配。首期建议使用规则引擎，后续再叠加 ML/LLM：

- 趋势状态：强多、弱多、震荡、弱空、强空。
- 波动率状态：极低、偏低、中性、偏高、极高。
- 曲面状态：正向期限结构、倒挂、put skew 高、call skew 高。
- 流动性状态：优、可交易、边缘、不可交易。
- 事件状态：普通日、节假日前后、财报窗口、宏观事件窗口。

### 9.3 Strategy Mapping Rules

首期使用“规则优先”的方式，把已有策略模板与市场状态做稳定映射：

- 强趋势 + 低 IV：优先买方策略，如 buy call spread、buy put spread。
- 趋势延续 + 中高 IV：优先方向性卖方价差，如 bull put spread、bear call spread。
- 恐慌急跌 + 高 IV + 强支撑：优先 sell put spread。
- 宽幅震荡 + 高 IV：优先 iron condor、short strangle 类中性卖方策略。
- 窄幅盘整 + 极低 IV：优先 long straddle、long strangle。
- 熊市反弹末端 + 中低 IV：优先 bear put spread。

### 9.4 Recommendation Scoring

每个候选推荐输出统一分数 `Recommendation Score = Context Fit + Edge Quality + Execution Quality - Risk Penalty`。

建议拆分为以下维度：

- Context Fit：当前市场状态与策略模板匹配度。
- Vol Edge：IV 水平、IV rank、term structure、skew 是否支持该策略。
- Liquidity Quality：spread、OI、activity ratio、tradability ratio。
- Historical Support：同类 regime 的历史表现和回测表现。
- Risk Penalty：事件风险、gamma 爆发风险、流动性不足、期限不合适。

可以进一步落成产品可读的权重视图：

- Environment Fit。
- Income or Convexity Quality。
- Greeks Advantage。
- Tradability。
- Risk Review。

用户不一定需要看全部内部公式，但需要知道该推荐高分是因为“环境匹配”还是因为“收益质量”，低分又主要低在哪里。

### 9.5 Seller Advantage Scoring

针对卖方推荐，单独维护四项优势判断：

- Delta advantage：价格到短腿的安全缓冲是否充足。
- Gamma advantage：临近到期或突发行情下的 gamma 风险是否可控。
- Vega advantage：当前 IV 是否足够高且有均值回归逻辑。
- Theta advantage：时间价值衰减是否足够有效。

卖方策略进入推荐池的基本条件：

- 四项中至少 3 项成立。
- 流动性达到最低可执行阈值。
- 不落在高风险事件窗口内，或已明确计入风险折扣。

### 9.6 DTE Recommendation Logic

卖方策略需要独立的 DTE 推荐逻辑：

- IV percentile < 40：偏短期限，约 2 周。
- IV percentile 40-60：约 3 周。
- IV percentile 60-70：约 1.5 个月。
- IV percentile 70-80：约 3 个月。
- IV percentile 80-100：约 3 到 6 个月，根据波动率极端程度延长。

这是首期规则版默认逻辑，后续可由历史回测统计校准。

### 9.7 Logical Agent Roles

外部设计中的 Multi-Agent 思路可以保留为“逻辑角色”，但首期实现应优先做成稳定的服务模块，而不是依赖 LLM 自由发挥。

建议保留以下角色分工：

- Market Scan：汇总候选标的与链质量。
- Regime Classification：识别趋势和波动环境。
- IV Analysis：判断 IV 状态、偏度和期限结构。
- Strategy Mapping：把环境映射为策略集合。
- Scoring：输出综合评分和子项分数。
- Critic：专门找致命缺陷和不推荐原因。
- Risk Guard：统一生成最大亏损、仓位提醒和风险提示。
- Output Formatter：把结构化结果转成用户可读推荐卡片。

### 9.8 Output Format

外部设计中的“四句法”非常适合新手理解，应当合并为推荐卡片标准格式：

- 你赚的是什么。
- 你最怕的是什么。
- 怎么进场。
- 怎么管理。

这四句应建立在结构化字段之上，而不是纯文案生成。这样既适合 UI，也适合 Copilot 和通知推送。

## 10. Functional Requirements

### 10.1 Current Functions to Reuse Directly

- 市场和数据就绪状态接口。
- 统一 bars、symbols、greeks、chain 查询接口。
- feature-store 快照和历史接口。
- 现有策略目录与回测引擎。

### 10.2 New Functions to Build

#### Product APIs

- `GET /api/v1/recommendations/opportunities`
  - 返回今日机会榜单。
- `GET /api/v1/recommendations/underlyings/{symbol}`
  - 返回某个标的的完整上下文摘要。
- `GET /api/v1/recommendations/strategies`
  - 返回策略推荐列表，支持按 market、underlying、intent、seller-only 过滤。
- `GET /api/v1/recommendations/sellers`
  - 返回卖方专项推荐。
- `GET /api/v1/recommendations/{id}/explanation`
  - 返回推荐解释与风险分析。
- `POST /api/v1/recommendations/backtest`
  - 对推荐结果触发标准化回测验证。

#### Internal Services

- Regime classifier service。
- Strategy mapping service。
- Recommendation scorer service。
- Recommendation explanation service。
- Recommendation cache/materialization job。

### 10.3 UI Functions

- Dashboard：今日机会概览。
- Watchlist：自选标的与策略提醒。
- Underlying detail：上下文、曲面、流动性、推荐。
- Strategy compare：多策略横向比较。
- Seller board：卖方专项排名页。
- Backtest detail：推荐到回测的联动页。
- Strategy library：策略知识卡片与学习入口。
- Copilot panel：自然语言提问与推荐解释。

### 10.4 Beginner-Facing Product Requirements

- 默认首页支持 Beginner Mode。
- Beginner Mode 默认隐藏过度复杂的链级细节，优先显示结论、风险和教学解释。
- Beginner Mode 默认只推荐有限风险或风险可明确定义的策略。
- 推荐卡片必须显示“1 张合约代表什么”“最大亏损是多少”“是否可能提前行权”。
- 支持按“新手可做”“有限风险”“不跨事件窗口”进行筛选。
- 推荐详情页必须能回链到对应策略卡片和基础术语解释。

### 10.5 Risk Guardrails and Hard Constraints

以下规则值得从外部设计中吸收，并改成平台级硬约束：

- 默认不推荐裸卖 call。
- MVP 阶段不将 0DTE 策略作为默认推荐结果。
- 距离到期过近时，对卖方策略自动加入 gamma 风险惩罚。
- 持仓窗口横跨重大事件时，必须展示事件风险警告，必要时直接降级或移除。
- 卖出腿可能提前行权的结构，必须明确提示美式期权行权风险。
- 所有推荐必须显示最大亏损、资金占用或保证金占用的近似值。
- 当推荐因 Critic 规则被移除时，应保留“为什么不推荐”的解释，而不是静默消失。

## 11. Data and System Architecture

### 11.1 Principle

不新建一套平行平台。产品层必须复用现有 `internal/api`、`internal/service`、`internal/dto`、`pkg/strategies`、`internal/backtest` 和 feature-store 能力。

### 11.2 Proposed Product Layer

在现有架构上新增 Recommendation Domain：

- `internal/recommendation/`
  - regime classification
  - strategy mapping
  - scoring
  - critic review
  - risk guard
  - explanation
  - seller filters
- `internal/service/recommendation.go`
  - 对外暴露 DTO 驱动服务。
- `internal/dto/recommendation.go`
  - 定义请求响应。
- `internal/api/recommendation.go`
  - 增加推荐相关 handler。

### 11.3 Processing Modes

- Online read mode：实时组合最新特征并生成推荐。
- Batch materialization mode：每日预生成重点标的推荐结果，加快首页响应。

### 11.4 Explainability Architecture

解释层分两级：

- Structured explanation：来自规则引擎和评分维度，稳定、可审计。
- Natural-language explanation：由 AI/LLM 将结构化结果转成自然语言，便于用户理解。

建议统一输出以下结构化解释字段：

- why_this_underlying
- why_this_strategy
- what_you_earn
- what_you_fear
- how_to_enter
- how_to_manage
- max_loss_summary
- event_risk_summary
- liquidity_summary

## 12. Metrics and Ranking Design

### 12.1 Recommendation KPIs

- Recommendation CTR。
- Recommendation to backtest conversion。
- Recommendation to watchlist save rate。
- Seller board usage share。
- 用户查看解释页占比。
- Beginner Mode usage share。
- 策略卡片点击率。
- Copilot 跟进提问率。

### 12.2 Strategy Quality KPIs

- 推荐后 N 日方向命中率。
- 推荐组合的平均盈亏比。
- 推荐组合的 realized drawdown。
- 推荐卖方组合的止损触发率。
- 推荐信号在不同 regime 下的稳定性。

### 12.3 System KPIs

- Recommendation API latency。
- 推荐结果覆盖率。
- 可执行候选占比。
- 特征更新 freshness。
- 因流动性或风险护栏而被拦截的推荐占比。
- 用户反馈“推荐无法执行”的比例。

## 13. Rollout Plan

### Phase 0: Knowledge and Safety Foundation

目标：先把推荐系统的学习入口和风险护栏立住。

交付内容：

- 策略知识卡片首版。
- 四句法输出模板。
- Beginner Mode 首版信息架构。
- 平台级风险提示与不推荐规则。

### Phase 1: Recommendation MVP

目标：先把现有基础设施变成可用产品。

交付内容：

- 重点标的机会榜单。
- 单标的上下文摘要。
- 基于规则引擎的策略推荐。
- 卖方专项推荐页。
- 推荐解释和基本风险说明。
- 新手友好的推荐卡片与策略卡片联动。

### Phase 2: Validation and Comparison

交付内容：

- 推荐联动标准化回测。
- 多策略对比页。
- 推荐结果历史命中统计。
- watchlist 与提醒功能。
- Critic review 与风险降级可视化。

### Phase 3: AI Agent Upgrade

交付内容：

- 基于结构化上下文的 Agent 分析。
- 自然语言问答，例如“为什么今天更适合卖 put spread”。
- 个性化偏好，例如只看卖方、只看高流动性、只看 Crypto。
- 面向新手的问答式教学引导。

### Phase 4: Market Expansion

交付内容：

- 更多指数、商品、全球市场接入。
- 更多策略模板。
- 更多事件上下文，包括 earnings、macro calendar、news factors。

## 14. Risks and Open Questions

### 14.1 Risks

- 不同市场的数据粒度和可用字段不一致，推荐逻辑需要 graceful degradation。
- 若解释层只用 LLM，结果可能不稳定，因此必须以结构化规则为主。
- 卖方推荐若忽略事件风险，容易产生错误的高胜率幻觉。
- 回测表现不能直接等价为未来收益，需要在产品文案上明确边界。
- 如果默认信息密度太高，新手会把平台当成“复杂数据终端”而不是“可学会的决策产品”。

### 14.2 Open Questions

- 首期重点推荐池要覆盖多少个 underlying。
- 财报、宏观事件和新闻因子接入优先级。
- 推荐评分中历史回测权重应占多少。
- 用户是否需要自定义风险等级和偏好模板。

## 15. Summary

Toktik 当前已经具备一个很强的期权基础设施内核：多市场数据、特征存储、策略库、回测引擎和统一 API。产品下一步不应继续只增加底层接口，而应进入“决策产品化”阶段。

本设计建议把平台升级为一个面向期权机会发现和策略推荐的智能系统。首期重点不是做复杂 AI，而是先用已有特征和经典策略知识，建立稳定、可解释、可验证的推荐引擎，再逐步增加 Agent 和个性化能力。