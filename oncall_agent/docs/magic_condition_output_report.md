# 魔方条件输出与资格管理详细报告

## 一、条件输出类型概述

魔方系统支持三种条件输出类型，用于控制用户获得活动机会（如抽奖机会、领取机会等）的方式：

### 1. 按固定值输出 (ConditionCalFixed)

- **计算公式**: `资格数 = score_result`
- **说明**: 满足条件即获得固定数量的活动机会
- **配置参数**:
  - `score_result`: 固定输出的资格数量
- **代码实现**: `addFixedNums()`
  ```go
  result = group.Getscore_result()
  c.FixedNums += int32(result)
  ```

### 2. 按比例输出 (ConditionCalRelated)

- **计算公式**: `资格数 = 关联条件项结果 × score_result`
- **说明**: 满足条件后，根据关联模块的int类型输出结果按比例计算活动机会
- **配置参数**:
  - `score_result`: 比例系数
  - `IsRelateOutput`: 标识哪个条件项作为比例基数
  - `RelateModuleId`: 关联模块ID
- **代码实现**: `addRelatedNums()`
  ```go
  result = cast.ToInt64(item.CurResult) * group.Getscore_result()
  c.RelatedNums += cast.ToInt32(result)
  ```
- **示例**: 用户每开通1次会员获得2次抽奖机会 → score_result=2，关联开通模块的"开通次数"条件项

### 3. 按消耗输出 (ConditionCalConsumed)

- **计算公式**: `资格数 = (关联条件项结果 ÷ consume_num_per_time) × score_result`
- **说明**: 满足条件并消耗指定数量后获得活动机会
- **配置参数**:
  - `score_result`: 每次消耗产生的资格数
  - `consume_num_per_time`: 每次消耗值（除数）
  - `IsRelateOutput`: 标识哪个条件项作为可消耗资源
  - `RelateModuleId`: 关联模块ID
- **代码实现**: `addConsumerNums()`
  ```go
  result = (cast.ToInt64(item.GetCurResult()) / item.Getconsume_num_per_time()) * group.Getscore_result()
  c.ConsumerNums += int32(result)
  ```
- **示例**: 用户每消耗10积分获得1次抽奖机会 → consume_num_per_time=10, score_result=1

## 二、资格数量管理机制

### 2.1 资格数量的组成

魔方系统中，模块的总可用资格数由以下四部分组成：

```go
总资格数 = FixedNums + RelatedNums + InnerQualiNums + ConsumerMakeQualiNums
```

#### 各部分说明：

1. **FixedNums** (固定输出资格数)
   - 来源：按固定值计算的条件组
   - 计算时机：条件计算阶段
   - 累加规则：所有满足条件的固定值条件组结果累加

2. **RelatedNums** (比例输出资格数)
   - 来源：按比例计算的条件组
   - 计算时机：条件计算阶段
   - 累加规则：所有满足条件的比例条件组结果累加

3. **InnerQualiNums** (内部维护资格数)
   - 来源：资格服务内部维护的资格池
   - 用途：用于特殊业务场景的资格补充
   - 存储：通过资格服务持久化

4. **ConsumerMakeQualiNums** (消耗产生的资格数)
   - 来源：用户消耗资源后产生的资格
   - 计算时机：消耗操作成功后
   - 存储：通过资格服务持久化

5. **UsedQualiNums** (已用资格数)
   - 记录：用户已经使用的资格数量
   - 更新时机：每次业务操作成功后
   - 存储：通过资格服务持久化

### 2.2 资格判断逻辑

```go
// 判断资格是否足够
isEnough = (FixedNums + RelatedNums + InnerQualiNums + ConsumerMakeQualiNums) > UsedQualiNums
```

- **资格足够**: 直接执行业务，并增加已用资格数
- **资格不足**: 判断是否可以消耗，如果可以则执行消耗逻辑

### 2.3 消耗逻辑流程

当资格不足时，系统会判断是否需要消耗：

1. **判断条件**:
   - 上游请求允许消耗 (`IsConsumeIfHas = true`)
   - 存在按消耗输出的条件组且满足条件
   - 消耗计算结果 > 0

2. **消耗流程**:

   ```
   a. 调用关联模块的消耗接口（写操作）
   b. 消耗成功后，追加消耗产生的资格数
   c. 增加已用资格数
   ```

3. **事务支持**:
   - 支持TCC（Try-Confirm-Cancel）事务模式
   - Try阶段：尝试消耗
   - Confirm阶段：确认消耗
   - Cancel阶段：回滚消耗和资格

## 三、主控资格管理

### 3.1 资格服务调用

魔方主控通过资格服务（quali_main.QualiServer）管理资格数据：

#### 读取资格

```go
GetQualiResult(ctx, magicCtx, tlog) -> QualiResult
// 返回: UsedNums, InnerNums, ConsumerMakeNums
```

#### 写入资格

```go
// 增加已用资格
AddUsedQuali(ctx, req, relyConf, num)

// 增加消耗产生的资格
AddQualiGeneratedByConsumption(ctx, req, relyConf, cdata)

// 回滚已用资格
RollUsedQuali(ctx, req, num)

// 回滚消耗产生的资格
RollQualiGeneratedByConsumption(ctx, req, cdata)
```

### 3.2 资格周期清零机制

魔方支持按周期自动清零已用资格数，通过模块配置的 `cycle_clear_type` 字段控制：

#### 清零类型枚举

```go
const (
    ClearTypeNone = 0  // 不清零
    ClearTypeDay  = 1  // 按天清零
    ClearTypeWeek = 2  // 按周清零
    ClearTypeMon  = 3  // 按月清零
)
```

#### 清零时间计算

资格数据存储在Redis中，通过设置不同的过期时间实现周期清零：

1. **资格Key过期时间** (keyInterval):
   - 默认：3年
   - 有活动时间：活动结束时间 + 6个月

2. **资格Filter过期时间** (filterInterval):
   - 不清零：同keyInterval
   - 按天清零：当前时间的第二天零点
   - 按周清零：当前时间的第二周的周一零点
   - 按月清零：当前时间的第二个月1号零点

#### 实现代码

```go
func (c *RelyConfig) GetQualiExpireInterval(now time.Time) (keyInterval, filterInterval time.Duration) {
    cycleClearType := gjson.Get(c.CurModData.GetExtConf(), "cycle_clear_type").Int()
    switch consts.EnCycleClearType(cycleClearType) {
    case consts.ClearTypeDay:
        filterInterval = utils.StartOfLastDay(now).Sub(now)
    case consts.ClearTypeWeek:
        filterInterval = utils.StartOfLastWeek(now).Sub(now)
    case consts.ClearTypeMon:
        filterInterval = utils.StartOfLastMonth(now).Sub(now)
    default:
        filterInterval = keyInterval
    }
    return keyInterval, filterInterval
}
```

### 3.3 资格数量的实时计算

每次条件计算时，系统会：

1. **查询资格服务**：获取已用资格数、内部维护资格数、消耗产生的资格数
2. **计算条件结果**：根据条件组配置计算固定输出、比例输出、消耗输出
3. **判断资格充足性**：总资格数 vs 已用资格数
4. **执行业务逻辑**：
   - 资格足够：直接执行，增加已用资格数
   - 资格不足：尝试消耗，消耗成功后增加消耗产生的资格数和已用资格数

## 四、条件组优先级与计算顺序

### 4.1 优先级作用

条件组的 `Priority` 字段控制条件组的计算顺序：

- **排序规则**: 在 `InitConditionGroups()` 初始化时按 `priority` 升序排序
- **计算顺序**: 值越小越先被计算
- **重要性**:
  - 确保条件计算的确定性和可预测性
  - 可将成本低或快速的条件前置以优化性能
  - 对于Int类型计算，顺序会影响累加逻辑和IsRelateOutput项的识别

### 4.2 条件项优先级

条件组内的条件项通过 `Sort` 字段控制计算顺序：

- **排序规则**: 按 `sort` 升序排序
- **计算顺序**: 值越小越先被计算
- **逻辑**: 条件组内是"与"关系，所有条件项都满足才算通过

### 4.3 IsRelateOutput的识别

在条件组计算过程中，系统会识别标记为 `IsRelateOutput=true` 的条件项：

```go
// 遍历条件项
for index, item := range group.Items {
    if item.IsRelateOutput {
        result = int32(index)  // 记录关联输出的条件项索引
    }
}
```

这个索引用于：

- 按比例输出：获取比例基数
- 按消耗输出：获取可消耗数量和消耗模块ID

## 五、完整业务流程示例

### 示例场景：抽奖模块

**配置**:

- 条件组1（固定值）：用户分享过 → 获得1次机会
- 条件组2（比例）：每开通1次会员 → 获得2次机会
- 条件组3（消耗）：每消耗10积分 → 获得1次机会

**用户状态**:

- 已分享：是
- 开通次数：3次
- 积分：25分
- 已用资格数：5次

**计算过程**:

1. **条件计算**:
   - FixedNums = 1 (分享条件满足)
   - RelatedNums = 3 × 2 = 6 (开通3次)
   - ConsumerNums = (25 ÷ 10) × 1 = 2 (可消耗2次)
   - 总资格数 = 1 + 6 + 0 + 0 = 7

2. **资格判断**:
   - 总资格数(7) > 已用资格数(5) → 资格足够

3. **执行业务**:
   - 执行抽奖业务
   - 增加已用资格数：5 → 6

4. **如果资格不足**:
   - 判断是否允许消耗
   - 调用积分模块消耗10积分
   - 增加消耗产生的资格数：+1
   - 增加已用资格数：+1

## 六、关键技术点

### 6.1 并发处理

- 使用 `trpc.GoAndWait()` 并发执行消耗和资格写入操作
- 保证数据一致性

### 6.2 幂等性

- 通过幂等时长配置避免重复操作
- 幂等请求返回成功

### 6.3 事务支持

- 支持TCC分布式事务
- Try-Confirm-Cancel三阶段提交
- 失败自动回滚

### 6.4 性能优化

- 条件优先级排序优化计算顺序
- 资格数据缓存在Redis
- 并发查询依赖模块条件

## 七、总结

魔方的条件输出与资格管理系统是一个完整的、灵活的营销活动资格控制方案：

1. **三种输出类型**满足不同业务场景需求
2. **资格服务**统一管理资格数据，支持周期清零
3. **主控服务**负责条件计算和资格判断
4. **消耗机制**支持资源消耗换取活动机会
5. **优先级控制**确保计算顺序的确定性和性能优化
6. **事务支持**保证数据一致性

这套机制使得魔方平台能够灵活配置各种复杂的营销活动规则，同时保证系统的稳定性和性能。
