# Java 后端面试深度复习指南

> **文档性质**: Java 语言方向面试复习专题，覆盖语言基础、JVM、并发、框架、高级特性
> **适用岗位**: 后端开发工程师（Java方向）— 抖音支付/飞书/TikTok直播/番茄小说等
> **最后更新**: 2026-03-08

---

## 一、Java 语言基础（必会）

### 1.1 集合框架

#### 核心体系

```
Collection
├── List
│   ├── ArrayList    ← 数组实现，随机访问O(1)，扩容1.5倍
│   ├── LinkedList   ← 双向链表，增删O(1)，随机访问O(n)
│   ├── Vector       ← 线程安全ArrayList（已淘汰）
│   └── CopyOnWriteArrayList  ← 写时复制，读多写少场景
├── Set
│   ├── HashSet      ← HashMap实现，元素唯一
│   ├── LinkedHashSet ← 维护插入顺序
│   └── TreeSet      ← 红黑树，有序
└── Queue
    ├── PriorityQueue ← 小顶堆
    ├── ArrayDeque    ← 双端队列，栈/队列两用
    └── LinkedBlockingQueue ← 阻塞队列，生产者消费者
    
Map
├── HashMap          ← 数组+链表+红黑树（1.8+）
├── LinkedHashMap    ← 维护插入/访问顺序（LRU基础）
├── TreeMap          ← 红黑树，有序
├── ConcurrentHashMap ← 分段锁(1.7)/CAS+synchronized(1.8)
└── Hashtable        ← 全表锁（已淘汰）
```

#### HashMap 深度（面试最高频）

| 知识点 | 要点 |
|--------|------|
| **底层结构** | JDK 1.8: 数组 + 链表 + 红黑树（链表长度≥8且数组长度≥64时转红黑树） |
| **哈希计算** | `(h = key.hashCode()) ^ (h >>> 16)` — 高16位参与运算，减少碰撞 |
| **数组下标** | `(n - 1) & hash` — 位运算代替取模，要求容量是2的幂 |
| **扩容** | 容量翻倍（×2），负载因子0.75；1.8采用高低位链表避免rehash死循环 |
| **线程不安全** | 1.7 扩容头插法可能链表成环导致死循环；1.8 并发put可能数据覆盖 |
| **红黑树退化** | 当节点数≤6时退化回链表（避免频繁转换） |

**面试高频问题**:
- Q: "HashMap 1.7和1.8的区别？" → 数据结构（链表→链表+红黑树）、扩容（头插→尾插）、hash计算优化
- Q: "HashMap为什么不是线程安全的？" → 并发put数据覆盖/扩容时链表成环（1.7）
- Q: "HashMap的key可以为null吗？" → 可以，hash为0，放在数组[0]位置

#### ConcurrentHashMap 深度

| 版本 | 实现方式 | 锁粒度 |
|------|---------|--------|
| **JDK 1.7** | Segment数组 + HashEntry链表 | 分段锁（Segment继承ReentrantLock），默认16段 |
| **JDK 1.8** | Node数组 + 链表 + 红黑树 | CAS + synchronized（锁单个桶头节点） |

**1.8 关键操作**:
- `put`: 空桶用CAS插入；非空桶用synchronized锁头节点
- `size`: baseCount + CounterCell[]（类似LongAdder的分段计数）
- `get`: 无锁读（volatile保证可见性）

**面试话术**: "1.8的ConcurrentHashMap放弃了分段锁，改用CAS+synchronized锁桶头节点的方式，锁粒度更细。统计size时采用了类似LongAdder的分段计数思想，避免了高并发下的竞争。"

### 1.2 String

| 知识点 | 要点 |
|--------|------|
| **不可变性** | `final char[]`(1.8) / `final byte[]`(1.9+)；安全性（哈希缓存/线程安全） |
| **字符串常量池** | 堆内存中的特殊区域；`intern()` 手动入池 |
| **拼接性能** | `+` 编译成StringBuilder（循环中依然低效）；推荐StringBuilder |
| **String vs StringBuilder vs StringBuffer** | 不可变 vs 可变非线程安全 vs 可变线程安全(synchronized) |
| **JDK9 Compact Strings** | byte[] + coder标志位，Latin1用1字节，节省内存 |

### 1.3 泛型

| 知识点 | 要点 |
|--------|------|
| **类型擦除** | 编译后泛型信息被擦除，运行时`List<String>`和`List<Integer>`是同一个类 |
| **桥接方法** | 编译器自动生成，保证多态性 |
| **通配符** | `? extends T`（上界，只读）/ `? super T`（下界，只写）— PECS原则 |
| **限制** | 不能new泛型数组、不能instanceof泛型类型、不能有泛型static字段 |

### 1.4 反射与动态代理

| 方式 | 实现原理 | 适用场景 | 性能 |
|------|---------|---------|------|
| **JDK动态代理** | 基于接口，`Proxy.newProxyInstance` + `InvocationHandler` | 接口代理 | 较好 |
| **CGLIB** | 基于继承，ASM字节码生成子类 | 无接口的类代理 | 首次较慢，运行时快 |
| **Javassist** | 字节码操作库 | 更灵活的字节码修改 | 中等 |

**面试话术**: "JDK动态代理要求目标类实现接口，通过Proxy类生成代理对象，在InvocationHandler中做增强逻辑。CGLIB通过ASM字节码技术生成目标类的子类，不需要接口。Spring AOP默认对有接口的用JDK代理，无接口的用CGLIB。"

### 1.5 异常体系

```
Throwable
├── Error（不可恢复）
│   ├── OutOfMemoryError  ← 堆内存不足
│   ├── StackOverflowError ← 栈深度超限（递归过深）
│   └── NoClassDefFoundError
└── Exception
    ├── RuntimeException（unchecked，不强制捕获）
    │   ├── NullPointerException
    │   ├── ArrayIndexOutOfBoundsException
    │   ├── ClassCastException
    │   ├── IllegalArgumentException
    │   └── ConcurrentModificationException
    └── checked Exception（必须捕获或声明）
        ├── IOException
        ├── SQLException
        └── ClassNotFoundException
```

### 1.6 I/O 模型

| 模型 | 特点 | Java实现 | 适用场景 |
|------|------|---------|---------|
| **BIO** | 同步阻塞，一连接一线程 | InputStream/OutputStream | 连接数少 |
| **NIO** | 同步非阻塞，多路复用 | Channel + Buffer + Selector | 高并发（Netty基础） |
| **AIO** | 异步非阻塞，OS回调 | AsynchronousChannel | 理论最优，但Linux实现不成熟 |

**NIO 三大核心**:
- **Channel**: 双向通道（ServerSocketChannel/SocketChannel/FileChannel）
- **Buffer**: 缓冲区（position/limit/capacity/flip/clear）
- **Selector**: 多路复用器，一个线程监听多个Channel的事件

**面试话术**: "NIO的核心是Selector多路复用，一个线程可以管理上千个连接。Netty在NIO基础上做了大量优化，包括Reactor线程模型、零拷贝、内存池等。至于AIO，Linux下的epoll实现已经足够高效，而且AIO的回调模型增加了编程复杂度，所以Netty选择了NIO。"

### 1.7 函数式编程（Java 8+）

| 特性 | 要点 |
|------|------|
| **Lambda** | `(参数) -> {表达式}`，本质是函数式接口的匿名实现 |
| **Stream** | 惰性求值（中间操作不执行，终端操作触发）；支持parallel并行流 |
| **Optional** | 防NPE容器，`of/ofNullable/orElse/orElseGet/map/flatMap` |
| **方法引用** | `类名::方法名`（静态/实例/构造器引用） |
| **函数式接口** | @FunctionalInterface：Predicate/Function/Consumer/Supplier |

**并行流陷阱**: 
- 使用共享ForkJoinPool.commonPool()，默认线程数=CPU核数-1
- 有状态操作（sorted/distinct）会失去并行优势
- 小数据量并行反而更慢（线程调度开销）

---

## 二、JVM 深度（高频重点）

### 2.1 JVM 内存结构

```
┌─────────────────────────────────────────┐
│              JVM 内存结构                 │
├─────────────────────────────────────────┤
│  线程私有                                │
│  ┌──────────────┐ ┌──────────────────┐  │
│  │  程序计数器    │ │  虚拟机栈         │  │
│  │  (PC Register) │ │  (栈帧=局部变量表  │  │
│  │  当前指令地址   │ │  +操作数栈+动态链接│  │
│  └──────────────┘ │  +返回地址)        │  │
│                    └──────────────────┘  │
│  ┌──────────────────────────────────┐   │
│  │  本地方法栈 (Native Method Stack)  │   │
│  └──────────────────────────────────┘   │
├─────────────────────────────────────────┤
│  线程共享                                │
│  ┌──────────────────────────────────┐   │
│  │  堆 (Heap) — 对象实例分配           │   │
│  │  ┌─────────┐ ┌──────────────┐    │   │
│  │  │ 新生代   │ │   老年代      │    │   │
│  │  │Eden│S0│S1│ │  (Old Gen)   │    │   │
│  │  └─────────┘ └──────────────┘    │   │
│  └──────────────────────────────────┘   │
│  ┌──────────────────────────────────┐   │
│  │  元空间 (Metaspace) — 类元信息     │   │
│  │  (JDK8+，使用本地内存，替代永久代)   │   │
│  └──────────────────────────────────┘   │
├─────────────────────────────────────────┤
│  直接内存 (Direct Memory)                │
│  NIO的DirectByteBuffer使用，不受GC管理    │
└─────────────────────────────────────────┘
```

### 2.2 对象内存布局

```
┌──────────────────────────────────────┐
│             对象头 (Header)            │
│  ┌─────────────────────────────────┐ │
│  │ Mark Word (8字节/64位)           │ │
│  │ 哈希码|GC年龄|锁状态|线程ID       │ │
│  ├─────────────────────────────────┤ │
│  │ 类型指针 (4/8字节)               │ │
│  │ 指向类的元数据(Klass Pointer)     │ │
│  ├─────────────────────────────────┤ │
│  │ 数组长度 (4字节，仅数组对象)       │ │
│  └─────────────────────────────────┘ │
├──────────────────────────────────────┤
│             实例数据 (Instance Data)   │
│  字段按类型重排(long/double→int/float  │
│  →short/char→byte/boolean→ref)       │
├──────────────────────────────────────┤
│             对齐填充 (Padding)         │
│  对象大小必须是8字节的整数倍            │
└──────────────────────────────────────┘
```

**Mark Word 锁状态变化（64位）**:

| 锁状态 | 25bit | 31bit | 1bit (偏向) | 4bit (GC年龄) | 2bit (锁标志) |
|--------|-------|-------|:-----------:|:-------------:|:------------:|
| 无锁 | unused | hashcode | 0 | age | 01 |
| 偏向锁 | 线程ID(54bit) | Epoch(2bit) | 1 | age | 01 |
| 轻量级锁 | 指向栈中Lock Record的指针(62bit) | — | — | — | 00 |
| 重量级锁 | 指向Monitor的指针(62bit) | — | — | — | 10 |
| GC标记 | — | — | — | — | 11 |

**项目关联**: 项目使用 **JOL (Java Object Layout)** 工具分析对象内存布局，用于监控玩家数据对象大小（超120KB触发告警）。

### 2.3 垃圾回收

#### GC Roots

- 虚拟机栈中引用的对象（局部变量）
- 方法区中类静态属性引用的对象
- 方法区中常量引用的对象
- 本地方法栈中JNI引用的对象
- synchronized锁持有的对象
- 活跃线程(Thread)对象

#### 垃圾回收算法

| 算法 | 原理 | 优点 | 缺点 |
|------|------|------|------|
| **标记-清除** | 标记存活对象，清除未标记 | 简单 | 内存碎片 |
| **标记-复制** | 存活对象复制到另一半，清除原空间 | 无碎片 | 空间利用率50% |
| **标记-整理** | 存活对象向一端移动，清除边界外 | 无碎片，空间利用率高 | 移动对象开销大 |

#### G1 收集器详解（项目实际使用）

```
┌──────────────────────────────────────────┐
│              G1 堆内存布局                  │
│                                           │
│  ┌───┐┌───┐┌───┐┌───┐┌───┐┌───┐┌───┐    │
│  │ E ││ E ││ S ││ O ││ O ││ H ││ E │    │
│  └───┘└───┘└───┘└───┘└───┘└───┘└───┘    │
│  ┌───┐┌───┐┌───┐┌───┐┌───┐┌───┐┌───┐    │
│  │ O ││ 空 ││ E ││ O ││ S ││ 空 ││ O │    │
│  └───┘└───┘└───┘└───┘└───┘└───┘└───┘    │
│                                           │
│  E=Eden  S=Survivor  O=Old  H=Humongous   │
│  每个Region默认1-32MB，相同代不需要连续      │
└──────────────────────────────────────────┘
```

**G1 四个阶段**:

| 阶段 | 触发条件 | STW | 回收范围 | 说明 |
|------|---------|:---:|---------|------|
| **Young GC** | Eden区满 | ✅ | 所有Young Region | 存活对象复制到Survivor/Old |
| **并发标记** | 堆使用率达IHOP阈值(默认45%) | 部分STW | — | 三色标记+SATB快照 |
| **Mixed GC** | 并发标记完成后 | ✅ | Young + 部分Old | 选择回收价值最高的Old Region |
| **Full GC** | Mixed GC回收不够快 | ✅ | 全堆 | **应极力避免** |

**三色标记 + SATB**:
- **白色**: 未被访问（标记结束后回收）
- **灰色**: 已访问但子引用未全部扫描
- **黑色**: 已访问且子引用全部扫描
- **SATB (Snapshot At The Beginning)**: 标记开始时保存引用快照，并发修改通过写屏障记录到SATB队列

**项目中G1调优参数**:
```
-Xmx4g -Xms4g                    # 堆大小
-XX:+UseG1GC                      # 使用G1
-XX:MaxGCPauseMillis=200          # 目标停顿时间200ms
-XX:G1HeapRegionSize=4m           # Region大小
-XX:InitiatingHeapOccupancyPercent=45  # 触发并发标记的阈值
-XX:G1ReservePercent=10           # 预留空间防止晋升失败
```

**面试话术**: "我们项目使用G1 GC，通过调整MaxGCPauseMillis控制停顿时间目标在200ms以内。G1的核心优势是可预测的停顿时间模型——它会优先回收垃圾最多的Region（Garbage First的含义）。在实际调优中，我们主要关注IHOP阈值和Region大小的设置，避免触发Full GC。"

#### ZGC（了解）

| 特性 | G1 | ZGC |
|------|-----|-----|
| **停顿时间** | 几十到几百ms | <1ms（亚毫秒级） |
| **堆大小支持** | TB级 | 16TB |
| **核心技术** | SATB + 写屏障 | **着色指针** + **读屏障** + **多重映射** |
| **适用版本** | JDK 7+ | JDK 11+(实验)、JDK 15+(生产) |

### 2.4 类加载机制

#### 类加载过程

```
加载(Loading) → 验证(Verification) → 准备(Preparation)
→ 解析(Resolution) → 初始化(Initialization)
→ 使用(Using) → 卸载(Unloading)
```

- **加载**: 通过全限定名获取二进制字节流 → 转为方法区的类结构 → 生成Class对象
- **准备**: 类变量分配内存并设**零值**（final static 除外，直接赋值）
- **初始化**: 执行`<clinit>`方法（static块 + static变量赋值）

#### 双亲委派模型

```
Bootstrap ClassLoader（启动类加载器，C++实现）
    ↑ 委派
Extension ClassLoader（扩展类加载器）
    ↑ 委派
Application ClassLoader（应用类加载器）
    ↑ 委派
Custom ClassLoader（自定义类加载器）
```

**打破双亲委派的场景**:
1. **SPI机制**: JDBC的Driver加载（Bootstrap加载的接口，需要App ClassLoader加载实现类）→ 线程上下文类加载器
2. **热加载/热部署**: Tomcat的WebappClassLoader（每个Web应用独立加载）
3. **模块化**: OSGi 网状加载
4. **项目中**: Groovy脚本热更新 — 每次加载新脚本需要新的ClassLoader

### 2.5 JMM（Java内存模型）

#### happens-before 八大规则
1. **程序顺序规则**: 同一线程中，前面的操作 happens-before 后面的操作
2. **volatile规则**: volatile写 happens-before 后续volatile读
3. **锁规则**: unlock happens-before 后续lock
4. **传递性规则**: A hb B, B hb C → A hb C
5. **线程启动规则**: Thread.start() hb 该线程的所有操作
6. **线程终止规则**: 线程的所有操作 hb Thread.join()
7. **中断规则**: Thread.interrupt() hb 被中断线程检测到中断
8. **对象终结规则**: 构造函数执行结束 hb finalize()

#### volatile

| 特性 | 说明 |
|------|------|
| **可见性** | 写volatile变量时，强制刷新到主内存；读时强制从主内存读取 |
| **有序性** | 禁止指令重排（内存屏障：StoreStore/StoreLoad/LoadLoad/LoadStore） |
| **不保证原子性** | `i++` 不是原子操作（读-改-写三步） |

**DCL（Double-Check Locking）单例为什么需要volatile**:
```java
private volatile static Singleton instance; // 必须volatile
public static Singleton getInstance() {
    if (instance == null) {            // 第一次检查（无锁）
        synchronized (Singleton.class) {
            if (instance == null) {    // 第二次检查（有锁）
                instance = new Singleton(); 
                // 这步可能被重排序：
                // 1.分配内存 → 2.初始化 → 3.指向地址
                // 重排为: 1→3→2，其他线程可能拿到未初始化的对象
            }
        }
    }
    return instance;
}
```

### 2.6 JVM 调优与排障

#### 常用 JVM 参数

| 参数 | 说明 |
|------|------|
| `-Xms` / `-Xmx` | 堆初始/最大大小（建议设相同，避免GC后resize） |
| `-Xmn` | 新生代大小 |
| `-Xss` | 线程栈大小（默认512K-1M） |
| `-XX:MetaspaceSize` | 元空间初始大小 |
| `-XX:+PrintGCDetails` | 打印GC详情 |
| `-XX:+HeapDumpOnOutOfMemoryError` | OOM时自动dump堆 |

#### 线上排障工具链

| 工具 | 用途 | 典型命令 | 项目实践 |
|------|------|---------|---------|
| **jps** | 查看Java进程 | `jps -lv` | 快速定位进程PID |
| **jstack** | 线程快照（排查死锁/CPU高） | `jstack -l <pid>` | CPU 100%排查 |
| **jmap** | 内存快照 | `jmap -dump:format=b,file=heap.hprof <pid>` | 内存泄漏排查 |
| **jstat** | GC统计 | `jstat -gcutil <pid> 1000` | GC频率监控 |
| **Arthas** | 在线诊断神器 | `watch/trace/dashboard/thread` | **项目主力工具** |
| **async-profiler** | 火焰图生成 | `./profiler.sh -d 30 -f flame.html <pid>` | 热点函数定位 |
| **GCViewer** | GC日志可视化分析 | 导入gc.log | GC调优辅助 |

#### CPU 100% 排查流程（面试必会）

```
1. top 定位高CPU的Java进程PID
2. top -Hp <PID> 定位高CPU的线程TID
3. printf "%x" <TID> 转16进制
4. jstack <PID> | grep <16进制TID> -A 30 查看线程堆栈
   或者直接用 Arthas: thread -n 3 (显示CPU最高的3个线程)
5. 分析堆栈 → 定位到具体代码行
```

#### 内存泄漏排查流程

```
1. jstat -gcutil <pid> 1000 观察Old区是否持续增长
2. jmap -dump:format=b,file=heap.hprof <pid> 导出堆快照
   或者 Arthas: heapdump /tmp/heap.hprof
3. MAT(Eclipse Memory Analyzer) 分析:
   - Leak Suspects Report → 自动分析嫌疑对象
   - Dominator Tree → 找出占用最大的对象
   - 查看GC Roots引用链 → 定位泄漏路径
```

**项目中的实际案例**: "使用Arthas的`watch`命令监控某个方法的返回值和入参，发现一个缓存没有设TTL导致对象持续累积。配合火焰图发现该方法的CPU占比从5%上升到了30%。通过加入LRU淘汰策略+TTL过期，老年代增长率从每小时3%降到0.1%。"

---

## 三、Java 并发编程（高频重点）

### 3.1 线程基础

#### 线程状态（6种）

```
NEW → (start()) → RUNNABLE ↔ (I/O/synchronized) → BLOCKED
                     ↕ (wait/join/sleep/park)
                  WAITING / TIMED_WAITING
                     ↓ (run完成/异常)
                  TERMINATED
```

| 状态 | 触发条件 |
|------|---------|
| **NEW** | Thread创建，未start |
| **RUNNABLE** | 调用start()，包括Ready和Running |
| **BLOCKED** | 等待synchronized锁 |
| **WAITING** | `Object.wait()`/`Thread.join()`/`LockSupport.park()` |
| **TIMED_WAITING** | `sleep(n)`/`wait(n)`/`join(n)`/`parkNanos(n)` |
| **TERMINATED** | run方法执行完毕或异常退出 |

### 3.2 synchronized 深度

#### 锁升级过程

```
无锁 → 偏向锁 → 轻量级锁 → 重量级锁
(单线程)  (一个线程)  (短暂竞争)  (激烈竞争)
         CAS记录    CAS自旋     OS Mutex
         线程ID     Lock Record  Monitor
```

| 锁级别 | 适用场景 | 实现方式 | 性能 |
|--------|---------|---------|------|
| **偏向锁** | 只有一个线程访问 | Mark Word记录线程ID，无CAS操作 | 最快 |
| **轻量级锁** | 两个线程交替执行（无竞争） | CAS将Mark Word复制到栈帧Lock Record | 快 |
| **重量级锁** | 多线程同时竞争 | OS Mutex互斥量，涉及用户态/内核态切换 | 慢 |

#### synchronized vs ReentrantLock

| 维度 | synchronized | ReentrantLock |
|------|-------------|---------------|
| **实现** | JVM内置（monitorenter/monitorexit） | API层面（AQS） |
| **可中断** | 不可中断 | `lockInterruptibly()` 可中断 |
| **超时** | 不支持 | `tryLock(timeout)` |
| **公平性** | 非公平 | 可配置公平/非公平 |
| **条件变量** | 单个wait/notify | 多个Condition（精确唤醒） |
| **锁释放** | 自动释放 | 必须手动unlock（try-finally） |
| **性能** | JDK6+优化后相当 | 相当 |

### 3.3 AQS（AbstractQueuedSynchronizer）

```
AQS核心结构:
┌─────────────────────────────────┐
│  state (volatile int)           │  ← 同步状态
│  ┌──────────────────────────┐   │
│  │  CLH双向队列              │   │
│  │  head ← Node ← Node ← tail │
│  │  (每个Node封装一个等待线程) │   │
│  └──────────────────────────┘   │
│  exclusiveOwnerThread           │  ← 持有锁的线程
└─────────────────────────────────┘
```

**基于AQS的实现类**:
- `ReentrantLock`: state=0未锁, state>0已锁(可重入次数)
- `CountDownLatch`: state=计数值, countDown减1, await等state=0
- `Semaphore`: state=许可数, acquire减1, release加1
- `ReentrantReadWriteLock`: state高16位读计数, 低16位写计数

**面试话术**: "AQS的核心是一个volatile的state变量和一个CLH变种的双向等待队列。获取锁时先CAS修改state，失败则封装为Node加入队列尾部并park挂起。释放锁时修改state并unpark队列头部的后继节点。ReentrantLock、CountDownLatch、Semaphore底层都是基于AQS实现的。"

### 3.4 线程池

#### 核心参数（ThreadPoolExecutor）

```java
public ThreadPoolExecutor(
    int corePoolSize,      // 核心线程数（常驻）
    int maximumPoolSize,   // 最大线程数
    long keepAliveTime,    // 非核心线程空闲存活时间
    TimeUnit unit,
    BlockingQueue<Runnable> workQueue,  // 任务队列
    ThreadFactory threadFactory,        // 线程工厂
    RejectedExecutionHandler handler    // 拒绝策略
)
```

#### 任务提交流程

```
提交任务 → 核心线程数未满？→ 创建核心线程执行
                ↓ 满了
           队列未满？→ 加入队列等待
                ↓ 满了
           最大线程数未满？→ 创建非核心线程执行
                ↓ 满了
           执行拒绝策略
```

#### 四种拒绝策略

| 策略 | 行为 | 适用场景 |
|------|------|---------|
| **AbortPolicy** | 抛RejectedExecutionException（**默认**） | 需要感知拒绝的场景 |
| **CallerRunsPolicy** | 提交线程自己执行 | 不丢弃，有反压效果 |
| **DiscardPolicy** | 静默丢弃 | 允许丢失 |
| **DiscardOldestPolicy** | 丢弃队列最老的任务 | 允许丢失旧任务 |

#### 线程池大小设置经验

| 任务类型 | 推荐公式 | 说明 |
|---------|---------|------|
| **CPU密集型** | N+1 (N=CPU核数) | 多1个防止偶尔的页缺失 |
| **IO密集型** | 2N 或 N/(1-阻塞比) | IO等待时间占比越高，线程越多 |
| **混合型** | 根据实际测试调整 | 先预估，再压测验证 |

#### ForkJoinPool（项目实践）

```java
// 项目中用于并行计算场景
ForkJoinPool pool = new ForkJoinPool(Runtime.getRuntime().availableProcessors());
// 任务拆分：大任务 → 若干小任务(fork) → 合并结果(join)
// 核心：工作窃取算法(Work-Stealing) — 空闲线程从其他线程的队列尾部偷任务
```

**项目关联**: 项目中使用 ForkJoin 进行批量数据处理的并行计算，利用工作窃取算法提升多核利用率。

### 3.5 CAS 与原子类

| 概念 | 说明 |
|------|------|
| **CAS** | Compare And Swap，比较并交换（CPU原子指令cmpxchg） |
| **ABA问题** | 值从A→B→A，CAS检测不到变化 |
| **解决ABA** | `AtomicStampedReference`（带版本号）/ `AtomicMarkableReference`（带标记） |
| **自旋开销** | 高并发下CAS失败不断重试，浪费CPU |

| 原子类 | 说明 |
|--------|------|
| `AtomicInteger/Long` | 基础原子整数 |
| `AtomicReference` | 原子引用 |
| `AtomicStampedReference` | 带版本号，解决ABA |
| `LongAdder` | **高并发计数器**（分段计数，优于AtomicLong） |
| `LongAccumulator` | 自定义累积函数 |

**LongAdder vs AtomicLong**: "LongAdder采用分段计数的思想（类似ConcurrentHashMap的分段锁），维护一个base值和多个Cell。高并发时不同线程CAS不同的Cell，最后sum汇总。这样减少了CAS的竞争，在高并发写多读少的场景下性能远优于AtomicLong。"

### 3.6 ThreadLocal

```
Thread 对象
  └── threadLocals (ThreadLocalMap)
        ├── Entry[0]: key=ThreadLocal@A (弱引用), value=值A
        ├── Entry[1]: key=ThreadLocal@B (弱引用), value=值B
        └── ...
```

**内存泄漏原因**:
1. ThreadLocalMap的key是**弱引用**（WeakReference<ThreadLocal<?>>）
2. 当ThreadLocal对象没有外部强引用时，GC会回收key → key变为null
3. 但value是**强引用**，不会被回收 → **value泄漏**
4. 只有在下次调用ThreadLocal的get/set/remove时才会清理key为null的Entry

**最佳实践**: 用完后必须调用`remove()`，特别是在线程池中（线程会复用）。

### 3.7 CompletableFuture

```java
// 异步编排示例
CompletableFuture<User> userFuture = CompletableFuture.supplyAsync(() -> getUser(id));
CompletableFuture<List<Order>> orderFuture = CompletableFuture.supplyAsync(() -> getOrders(id));

// 两个异步任务完成后合并
CompletableFuture<UserDetail> result = userFuture
    .thenCombine(orderFuture, (user, orders) -> new UserDetail(user, orders))
    .exceptionally(ex -> defaultUserDetail())  // 异常处理
    .orTimeout(3, TimeUnit.SECONDS);            // 超时控制(JDK9+)
```

**项目关联**: 项目中的 `CoroutineAsync` 协程异步工具在概念上类似CompletableFuture的异步编排，但底层基于Kona Fiber协程实现。

### 3.8 协程 / 虚拟线程（项目核心亮点）

#### 传统线程 vs 协程 vs 虚拟线程

| 维度 | OS线程(Thread) | 协程(Fiber/goroutine) | 虚拟线程(JDK21 Loom) |
|------|---------------|---------------------|---------------------|
| **调度** | OS内核调度 | 用户态调度 | JVM调度 |
| **栈大小** | ~1MB(固定) | ~数KB(可增长) | ~数KB(可增长) |
| **创建开销** | 高（系统调用） | 极低 | 极低 |
| **上下文切换** | ~1-10μs（内核态） | ~100ns（用户态） | ~100ns |
| **并发量** | 数千级 | **数十万级** | 百万级 |
| **模型** | 1:1 (线程:OS线程) | **N:M** (协程:OS线程) | N:M |

#### 项目中的协程实现（Kona Fiber）

```
项目协程架构（N:M模型）:
┌───────────────────────────────────────┐
│  N 个协程 (Fiber/Coroutine)            │
│  ┌─────┐ ┌─────┐ ┌─────┐ ... ┌─────┐ │
│  │ F1  │ │ F2  │ │ F3  │     │ Fn  │ │
│  └──┬──┘ └──┬──┘ └──┬──┘     └──┬──┘ │
│     └───────┴───────┴─────┬─────┘     │
│                           │ 用户态调度  │
│     ┌─────────────────────┘           │
│     ↓                                 │
│  M 个载体线程 (Carrier Thread)         │
│  ┌─────┐ ┌─────┐ ... ┌─────┐         │
│  │ T1  │ │ T2  │     │ Tm  │         │
│  └─────┘ └─────┘     └─────┘         │
│           (M << N)                     │
└───────────────────────────────────────┘
```

**面试话术**: "我们项目使用腾讯的Kona Fiber实现了类似Go goroutine的N:M协程模型。多个协程被调度到少量的载体线程上执行，协程遇到IO操作（如Redis/TcaplusDB查询）时会自动挂起让出载体线程，等IO完成后再恢复执行。这样单线程就能支撑数万并发，完全避免了OS线程的上下文切换开销。这个设计理念和JDK 21的虚拟线程（Project Loom）是一致的。"

---

## 四、Java 常用框架

### 4.1 Spring Boot / Spring

#### IoC 容器

| 知识点 | 要点 |
|--------|------|
| **Bean生命周期** | 实例化 → 属性注入 → Aware接口 → BeanPostProcessor前置 → InitializingBean/init-method → BeanPostProcessor后置 → 使用 → DisposableBean/destroy-method |
| **作用域** | singleton(默认)/prototype/request/session/application |
| **循环依赖** | **三级缓存**: singletonObjects(成品) → earlySingletonObjects(半成品) → singletonFactories(工厂) |
| **自动装配** | @Autowired(byType) / @Resource(byName) / @Qualifier(指定名称) |

**循环依赖解决过程**:
```
A创建 → 放入三级缓存(工厂) → 注入B → B创建 → 放入三级缓存
→ B注入A → 从三级缓存获取A的早期引用 → 放入二级缓存
→ B完成 → A注入B → A完成 → 从二级缓存移到一级缓存
```

> 注意：构造器注入无法解决循环依赖；prototype作用域无法解决循环依赖。

#### AOP

| 概念 | 说明 |
|------|------|
| **切面(Aspect)** | 横切关注点的模块化（如日志、事务） |
| **连接点(JoinPoint)** | 程序执行的某个点（方法调用/异常抛出） |
| **切入点(Pointcut)** | 匹配连接点的表达式 |
| **通知(Advice)** | Before/After/Around/AfterReturning/AfterThrowing |
| **底层实现** | 有接口→JDK代理；无接口→CGLIB代理 |

#### Spring Boot 自动配置原理

```
@SpringBootApplication
  ├── @SpringBootConfiguration ← 标识配置类
  ├── @ComponentScan ← 包扫描
  └── @EnableAutoConfiguration
        └── @Import(AutoConfigurationImportSelector.class)
              └── 读取 META-INF/spring.factories 
                  → 加载所有AutoConfiguration类
                  → @Conditional条件过滤 → 只注册满足条件的Bean
```

### 4.2 Netty（面试高频）

#### Reactor 线程模型

```
┌─────────────────────────────────────────┐
│           Netty Reactor模型              │
│                                         │
│  BossGroup (1个NioEventLoop)            │
│  ┌────────────────────┐                 │
│  │ Selector → Accept  │                 │
│  │ 接受新连接 → 注册到Worker │            │
│  └────────────────────┘                 │
│           ↓ 分发                         │
│  WorkerGroup (N个NioEventLoop)          │
│  ┌────────┐ ┌────────┐ ┌────────┐      │
│  │Selector│ │Selector│ │Selector│      │
│  │ 读写IO │ │ 读写IO │ │ 读写IO │      │
│  │Pipeline│ │Pipeline│ │Pipeline│      │
│  └────────┘ └────────┘ └────────┘      │
│                                         │
│  Pipeline: Handler1 → Handler2 → ...    │
│  (编解码器 → 业务处理器)                  │
└─────────────────────────────────────────┘
```

**Netty 零拷贝**:
1. `FileChannel.transferTo()` — OS级零拷贝（sendfile系统调用）
2. `CompositeByteBuf` — 逻辑合并多个Buffer，无需内存拷贝
3. `slice()` — 共享底层数组，创建子Buffer无拷贝
4. `DirectByteBuf` — 堆外内存，减少JVM堆到OS的拷贝

### 4.3 MyBatis

| 知识点 | 要点 |
|--------|------|
| **一级缓存** | SqlSession级别，默认开启；同一Session中相同查询直接返回缓存 |
| **一级缓存坑** | 不同SqlSession间不共享；增删改会清空一级缓存 |
| **二级缓存** | Mapper级别（namespace），需手动开启；跨SqlSession共享 |
| **二级缓存坑** | 多表关联查询可能脏读；分布式环境下不可靠 |
| **#{}和${}** | `#{}` 预编译参数化（防SQL注入）；`${}` 字符串拼接（用于动态表名/排序） |

---

## 五、面试高频场景题

### 5.1 "设计一个线程安全的单例"

```java
// 推荐：枚举单例（最简洁，天然防反射和序列化破解）
public enum Singleton {
    INSTANCE;
    public void doSomething() { ... }
}

// 或者：静态内部类（懒加载 + 线程安全）
public class Singleton {
    private Singleton() {}
    private static class Holder {
        private static final Singleton INSTANCE = new Singleton();
    }
    public static Singleton getInstance() {
        return Holder.INSTANCE;
    }
}
```

### 5.2 "手写一个简单的线程池"

```java
public class SimpleThreadPool {
    private final BlockingQueue<Runnable> taskQueue;
    private final List<WorkerThread> workers;
    private volatile boolean isShutdown = false;
    
    public SimpleThreadPool(int poolSize, int queueSize) {
        this.taskQueue = new LinkedBlockingQueue<>(queueSize);
        this.workers = new ArrayList<>(poolSize);
        for (int i = 0; i < poolSize; i++) {
            WorkerThread worker = new WorkerThread();
            workers.add(worker);
            worker.start();
        }
    }
    
    public void submit(Runnable task) {
        if (isShutdown) throw new IllegalStateException("Pool is shutdown");
        taskQueue.offer(task);
    }
    
    private class WorkerThread extends Thread {
        public void run() {
            while (!isShutdown || !taskQueue.isEmpty()) {
                try {
                    Runnable task = taskQueue.poll(1, TimeUnit.SECONDS);
                    if (task != null) task.run();
                } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                }
            }
        }
    }
}
```

### 5.3 "OOM的几种场景和排查思路"

| OOM类型 | 原因 | 排查方式 |
|---------|------|---------|
| **Java heap space** | 对象太多/太大，内存不够 | jmap dump → MAT分析大对象 |
| **GC overhead limit exceeded** | GC占用>98%时间但回收<2%内存 | 检查内存泄漏 |
| **Metaspace** | 类加载过多（动态代理/Groovy脚本） | `-XX:MaxMetaspaceSize`限制 + 检查ClassLoader泄漏 |
| **Direct buffer memory** | NIO DirectByteBuffer过多 | `-XX:MaxDirectMemorySize`限制 |
| **unable to create native thread** | 线程数超过OS限制 | 检查线程泄漏 / ulimit配置 |
| **StackOverflowError** | 递归过深 / 栈帧过大 | 检查递归逻辑 / 增大-Xss |

---

## 六、项目中的 Java 技术关联总结

| 项目技术 | 对应Java知识点 | 面试可延伸方向 |
|---------|---------------|--------------|
| **Kona Fiber协程** | 虚拟线程/用户态调度/N:M模型 | 并发模型、线程vs协程、JDK21 Loom |
| **G1 GC调优** | GC原理/三色标记/SATB/调优参数 | JVM内存模型、GC算法、ZGC |
| **Arthas诊断** | 线上排障/字节码增强 | CPU排查、内存泄漏、火焰图 |
| **ForkJoinPool** | 工作窃取算法/并行计算 | 线程池参数、任务拆分策略 |
| **CoLoadingCache(LRU)** | LinkedHashMap/ConcurrentHashMap | 缓存淘汰策略、线程安全集合 |
| **SingleFlight防击穿** | ConcurrentHashMap+CompletableFuture | 缓存一致性、并发控制 |
| **CacheLockAgent分布式锁** | ReentrantLock/CAS | AQS原理、分布式锁实现 |
| **Protobuf序列化** | 序列化机制/对比JSON/Thrift | 序列化原理、性能对比 |
| **Groovy热更新** | 类加载机制/打破双亲委派 | ClassLoader、SPI |
| **Netty网络层(gRPC)** | NIO/Reactor模型/零拷贝 | I/O模型、epoll |
| **JOL内存分析** | 对象内存布局/对齐填充 | Mark Word、锁升级 |
| **事件驱动EventSwitch** | 观察者模式/解耦 | 设计模式、Spring事件 |
| **令牌桶RateLimiter** | 原子操作/CAS | 限流算法、Guava RateLimiter |

---

> **使用方法**:
> 1. 按章节顺序复习，每章2-3小时
> 2. 重点关注有"面试话术"和"项目关联"标注的内容
> 3. 每个知识点都尝试关联到项目实践中去，面试时回答"不仅知道理论，还在项目中实际使用过"
> 4. JVM和并发是最高频考点，务必滚瓜烂熟