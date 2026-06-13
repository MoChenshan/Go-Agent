# Java 后端面试深度复习指南

> **文档性质**: Java 语言方向面试复习专题，覆盖语言基础、JVM、并发、框架、高级特性
> **适用岗位**: 后端开发工程师（Java方向）— 抖音支付/飞书/TikTok直播/番茄小说等
> **最后更新**: 2026-03-09

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

**HashMap put 流程详解（面试必会手画）**:

```
put(key, value)
  │
  ├── 1. table == null ? → resize() 初始化（懒加载，首次put才创建数组）
  │
  ├── 2. 计算hash → (n-1) & hash 定位桶下标i
  │
  ├── 3. table[i] == null ? → 直接new Node放入
  │
  ├── 4. table[i] != null（发生碰撞）
  │   ├── 4a. key完全相同(hash相同 && equals为true) → 覆盖旧value
  │   ├── 4b. 是TreeNode → 红黑树插入
  │   └── 4c. 是链表 → 尾插法遍历
  │       ├── 找到相同key → 覆盖
  │       └── 到链表尾部 → 插入新节点
  │           └── 链表长度 ≥ 8 ?
  │               ├── 数组长度 ≥ 64 → 转红黑树（treeifyBin）
  │               └── 数组长度 < 64 → 先扩容resize()
  │
  └── 5. ++size > threshold ? → resize() 扩容
```

**HashMap 扩容rehash过程（1.8优化）**:
- 1.7: 遍历每个节点重新计算`hash & (newCap-1)`，头插法（多线程下环形链表）
- 1.8: 利用`hash & oldCap`判断高低位——结果为0留在原位置，为1移到`原位置+oldCap`
- **巧妙之处**: 不需要重新计算hash，只需要看新增的那一个bit是0还是1

**面试高频问题（补充）**:
- Q: "HashMap 1.7和1.8的区别？" → 数据结构（链表→链表+红黑树）、扩容（头插→尾插）、hash计算优化
- Q: "HashMap为什么不是线程安全的？" → 并发put数据覆盖/扩容时链表成环（1.7）
- Q: "HashMap的key可以为null吗？" → 可以，hash为0，放在数组[0]位置
- Q: "HashMap的容量为什么必须是2的幂？" → `(n-1) & hash` 等价于 `hash % n`，位运算更快；且保证(n-1)的二进制全是1，hash值分布更均匀
- Q: "HashMap的负载因子为什么是0.75？" → 泊松分布下的折中：太小浪费空间，太大碰撞频繁。0.75时链表长度达到8的概率仅为0.00000006
- Q: "HashMap和Hashtable的区别？" → ①线程安全(全表锁) ②null key/value ③初始容量(16 vs 11) ④扩容(2倍 vs 2倍+1) ⑤继承关系不同
- Q: "HashMap 遍历时能修改吗？" → 不能，会抛ConcurrentModificationException（快速失败机制，modCount检测）
- Q: "LinkedHashMap 如何实现LRU？" → 构造时传`accessOrder=true`，重写`removeEldestEntry()`返回`size() > capacity`

#### fail-fast vs fail-safe

| 机制 | fail-fast（快速失败） | fail-safe（安全失败） |
|------|---------------------|---------------------|
| **代表** | HashMap/ArrayList 的 Iterator | ConcurrentHashMap/CopyOnWriteArrayList |
| **原理** | 遍历时检查modCount，不一致抛CME | 遍历的是**快照副本**，不受原集合修改影响 |
| **并发安全** | 不安全 | 安全 |
| **缺点** | 并发遍历易出错 | 内存开销大，不保证最新数据 |

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

#### String.intern() 深度（经典面试题）

```java
// JDK 1.6: intern()将字符串复制到永久代的常量池
// JDK 1.7+: intern()只在常量池中记录堆中字符串的引用（不复制）

String s1 = new String("abc");          // 堆中创建对象，常量池有"abc"
String s2 = s1.intern();                // 返回常量池中"abc"的引用
System.out.println(s1 == s2);           // false（s1指向堆，s2指向常量池）

String s3 = new String("a") + new String("b"); // 堆中"ab"，常量池无"ab"
String s4 = s3.intern();                // JDK7+：常量池记录s3的引用
System.out.println(s3 == s4);           // true（JDK7+ true，JDK6 false）

String s5 = "ab";                       // 从常量池获取（已指向s3）
System.out.println(s3 == s5);           // true
```

**经典面试题 — `new String("abc")` 创建了几个对象？**
- **1或2个**: ①如果常量池中没有"abc"，先在常量池创建一个字符串对象；②然后在堆中new一个String对象。如果常量池已有"abc"则只创建1个堆对象。

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

### 1.8 序列化机制

| 方式 | 速度 | 体积 | 跨语言 | 可读性 | 适用场景 |
|------|------|------|:------:|:------:|---------|
| **JDK序列化** | 慢 | 大 | ✗ | ✗ | 不推荐使用 |
| **JSON** | 中 | 中 | ✓ | ✓ | REST API/配置文件 |
| **Protobuf** | **快** | **小** | ✓ | ✗ | RPC通信（**项目使用**） |
| **Hessian** | 快 | 较小 | ✓ | ✗ | Dubbo默认 |
| **Kryo** | **最快** | 小 | ✗ | ✗ | 本地缓存序列化 |

**JDK序列化的坑**:
- `serialVersionUID`: 不显式定义会自动生成，类修改后反序列化失败
- `transient`: 标记的字段不参与序列化
- **安全风险**: 反序列化攻击（构造恶意字节流执行任意代码）→ 应使用白名单过滤

**面试话术**: "我们项目使用Protobuf作为RPC通信的序列化方案，相比JSON体积减少60-70%，序列化速度提升5-10倍。Protobuf采用Tag-Length-Value编码，整数用Varint变长编码节省空间，并且通过.proto文件保证前后兼容性。"

### 1.9 Object 类核心方法

| 方法 | 说明 | 面试要点 |
|------|------|---------|
| `equals()` | 判断逻辑相等 | 重写equals必须同时重写hashCode |
| `hashCode()` | 哈希值 | 两个对象equals为true，hashCode必须相同；反之不一定 |
| `toString()` | 字符串表示 | 默认`类名@十六进制哈希码` |
| `clone()` | 对象复制 | 浅拷贝（引用类型共享）；深拷贝需递归clone或序列化 |
| `wait()/notify()/notifyAll()` | 线程通信 | 必须在synchronized块中调用 |
| `finalize()` | GC前调用 | 不推荐使用，JDK9+已标记@Deprecated |
| `getClass()` | 获取运行时类 | 反射的入口 |

**equals() 和 == 的区别**:
- `==`: 基本类型比较值，引用类型比较**内存地址**
- `equals()`: Object默认实现是`==`，String/Integer等重写后比较**内容**

**hashCode 契约（面试必知）**:
1. equals为true → hashCode必须相同（一致性）
2. hashCode相同 → equals不一定为true（哈希碰撞）
3. 同一对象多次调用hashCode，结果不变（对象未修改时）
4. **为什么重写equals要重写hashCode？** → HashMap依赖hashCode定位桶，如果equals为true但hashCode不同，会被放到不同桶中，导致HashMap无法正确查找

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

### 2.2.1 对象创建过程（面试高频）

```
new 关键字触发对象创建:
  │
  ├── 1. 类加载检查: 检查类是否已加载、解析、初始化（否则先执行类加载）
  │
  ├── 2. 分配内存:
  │   ├── 指针碰撞 (Bump the Pointer): 堆内存规整时，指针向空闲方向移动
  │   └── 空闲列表 (Free List): 堆内存不规整时，维护可用内存块列表
  │   └── 并发安全保证:
  │       ├── CAS + 失败重试
  │       └── TLAB (Thread Local Allocation Buffer): 每个线程预分配一小块Eden区
  │
  ├── 3. 内存初始化零值: int→0, boolean→false, 引用→null（保证字段不赋值也能用）
  │
  ├── 4. 设置对象头: Mark Word + 类型指针 + （数组长度）
  │
  └── 5. 执行构造函数 <init>: 父类构造 → 实例变量赋值 → 构造方法体
```

**TLAB（Thread Local Allocation Buffer）详解**:
- 每个线程在Eden区预分配一小块私有空间（默认占Eden的1%）
- 对象优先在TLAB中分配，无需同步操作（避免CAS竞争）
- TLAB用完后，再CAS申请新的TLAB
- `-XX:+UseTLAB`（默认开启） / `-XX:TLABSize`

**面试话术**: "对象创建主要经过5步：类加载检查→分配内存→零值初始化→设置对象头→执行构造函数。内存分配时，为了保证并发安全，JVM使用TLAB技术——每个线程在Eden区有自己的私有分配缓冲区，避免了多线程分配内存时的CAS竞争。"

### 2.2.2 对象的访问定位

| 方式 | 实现 | 优点 | 缺点 |
|------|------|------|------|
| **句柄访问** | 栈中reference → 句柄池(对象指针+类型指针) → 实例数据 | GC移动对象时只需改句柄，reference不变 | 多一次间接寻址 |
| **直接指针** | 栈中reference → 对象实例（对象头中含类型指针） | 访问速度快，少一次间接寻址 | GC移动对象需更新reference |

> HotSpot 使用**直接指针**方式（速度优先）。

### 2.2.3 四种引用类型（面试高频）

| 引用类型 | 回收时机 | 用途 | 代码示例 |
|---------|---------|------|---------|
| **强引用** (Strong) | 永不回收（只要可达） | 正常变量引用 | `Object obj = new Object()` |
| **软引用** (Soft) | **内存不足时**才回收 | **缓存**（如图片缓存） | `SoftReference<byte[]> sr = new SoftReference<>(data)` |
| **弱引用** (Weak) | **下次GC**就回收（不管内存够不够） | **ThreadLocalMap的key**、WeakHashMap | `WeakReference<Object> wr = new WeakReference<>(obj)` |
| **虚引用** (Phantom) | 随时回收，**无法通过虚引用获取对象** | 跟踪GC回收动作（管理堆外内存） | `PhantomReference + ReferenceQueue` |

**ReferenceQueue（引用队列）**:
- 软引用/弱引用/虚引用被回收时，引用对象会被放入ReferenceQueue
- 通过轮询ReferenceQueue，可以感知对象被回收的事件
- **典型应用**: DirectByteBuffer用虚引用 + Cleaner机制释放堆外内存

**面试话术**: "四种引用的回收力度从强到弱：强引用不回收、软引用内存不够时回收、弱引用GC就回收、虚引用随时回收。ThreadLocal的key用弱引用是为了防止ThreadLocal对象本身泄漏；软引用适合做缓存；虚引用配合ReferenceQueue用于跟踪GC回收事件，比如NIO的DirectByteBuffer就用虚引用来释放堆外内存。"

### 2.2.4 逃逸分析与JIT优化

| 优化 | 说明 | 触发条件 |
|------|------|---------|
| **栈上分配** | 对象不逃逸出方法 → 直接在栈上分配（方法结束自动回收，无需GC） | 对象仅在方法内使用 |
| **标量替换** | 对象不逃逸 → 将对象拆解为基本类型变量，直接分配到栈/寄存器 | JIT编译优化 |
| **锁消除** | 对象不逃逸出线程 → 去掉synchronized | 如StringBuffer在方法内使用 |
| **锁粗化** | 连续多次加锁解锁同一个对象 → 合并为一次大锁 | 循环中反复加锁 |

```java
// 逃逸分析示例：
public void test() {
    // point不会逃逸出方法 → 可能被栈上分配或标量替换
    Point point = new Point(1, 2);
    int sum = point.x + point.y;
    // 标量替换后等价于：
    // int x = 1; int y = 2; int sum = x + y;
}
```

**JVM参数**: `-XX:+DoEscapeAnalysis`（默认开启）/ `-XX:+EliminateAllocations`（标量替换）/ `-XX:+EliminateLocks`（锁消除）

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

#### 各代GC收集器总结

| 收集器 | 算法 | 线程 | 适用区域 | 特点 |
|--------|------|------|---------|------|
| **Serial** | 复制 | 单线程 | 新生代 | 简单高效，Client模式默认 |
| **Serial Old** | 标记-整理 | 单线程 | 老年代 | Serial的老年代版 |
| **ParNew** | 复制 | 多线程 | 新生代 | Serial的多线程版，常配CMS |
| **Parallel Scavenge** | 复制 | 多线程 | 新生代 | **吞吐量优先**，自适应调节 |
| **Parallel Old** | 标记-整理 | 多线程 | 老年代 | 配合Parallel Scavenge |
| **CMS** | 标记-清除 | 并发 | 老年代 | **低停顿**，但有碎片和浮动垃圾（JDK14移除） |
| **G1** | 整体标记-整理/局部复制 | 并发 | 全堆(Region) | **可预测停顿**，JDK9+默认 |
| **ZGC** | 着色指针 | 并发 | 全堆 | **亚毫秒停顿**，JDK15+生产可用 |

**CMS（了解即可，已过时但面试偶尔问）**:
- 四个阶段: 初始标记(STW) → 并发标记 → 重新标记(STW) → 并发清除
- 缺点: ①内存碎片（标记-清除） ②浮动垃圾 ③CPU敏感 ④并发失败后退化Serial Old

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

### 2.7 永久代 vs 元空间（经典面试题）

| 维度 | 永久代 (PermGen, JDK7-) | 元空间 (Metaspace, JDK8+) |
|------|------------------------|--------------------------|
| **存储位置** | JVM堆内存中 | **本地内存（Native Memory）** |
| **默认大小** | 64M-256M，固定且容易OOM | 默认无上限（受物理内存限制） |
| **存储内容** | 类元信息 + 字符串常量池 + 静态变量 | 仅类元信息 |
| **GC回收** | Full GC时回收 | 当类加载器被回收时，其加载的类元数据才回收 |
| **调优参数** | `-XX:MaxPermSize` | `-XX:MaxMetaspaceSize`（建议设置，防止无限增长） |

**为什么JDK8要废弃永久代？**
1. 永久代大小难以确定，容易OOM
2. 永久代的GC效率低，Full GC才回收
3. 为融合HotSpot和JRockit做准备（JRockit没有永久代）
4. 字符串常量池移到堆中，可以被更高效地GC

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

#### sleep() vs wait() vs yield() vs join()（面试高频对比）

| 方法 | 所属类 | 释放锁 | 释放CPU | 唤醒方式 | 使用场景 |
|------|--------|:------:|:-------:|---------|---------|
| `sleep(ms)` | Thread | **✗** | ✓ | 超时自动/interrupt | 定时暂停 |
| `wait()` | Object | **✓** | ✓ | notify/notifyAll/interrupt | 线程间通信 |
| `yield()` | Thread | ✗ | ✓（提示性） | 立即可再次被调度 | 让出CPU给同优先级线程 |
| `join()` | Thread | ✗ | 当前线程阻塞 | 目标线程结束 | 等待另一个线程完成 |
| `park()` | LockSupport | ✗ | ✓ | unpark/interrupt | AQS底层挂起机制 |

**wait 和 sleep 的区别（必会）**:
1. `wait()` 是 Object 方法，`sleep()` 是 Thread 静态方法
2. `wait()` **释放锁**，`sleep()` **不释放锁**
3. `wait()` 必须在 synchronized 块中调用，`sleep()` 随处可用
4. `wait()` 需要 notify 唤醒，`sleep()` 到时间自动恢复
5. `wait()` 用于线程间通信，`sleep()` 用于定时暂停

**为什么 wait() 必须在 synchronized 块中？**
- 防止 **lost wake-up** 问题：如果不加锁，可能在判断条件和wait之间，另一个线程执行了notify，导致这次notify丢失，wait永远阻塞

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

**JDK15+取消偏向锁**: 偏向锁在现代多核CPU和高并发场景下，撤销偏向锁的开销反而更大（需要等到安全点），所以JDK15默认关闭，JDK18+直接废弃。

#### JVM 锁优化（面试经常追问）

| 优化技术 | 说明 | 示例 |
|---------|------|------|
| **锁消除** | JIT编译器判断锁对象不会逃逸出线程 → 直接去掉锁 | `StringBuffer sb = new StringBuffer(); sb.append("a");`（方法内局部变量） |
| **锁粗化** | 连续多次对同一对象加锁解锁 → 合并为一个大范围的锁 | 循环内反复synchronized同一对象 → 合并到循环外 |
| **自适应自旋** | 轻量级锁CAS失败后，根据历史成功率动态调整自旋次数 | 上次在同一个锁上自旋成功过 → 允许更多次自旋 |
| **锁降级** | 写锁降级为读锁（ReentrantReadWriteLock支持） | 先获取写锁 → 再获取读锁 → 释放写锁（安全降级） |

**synchronized 底层 Monitor 机制**:
```
Monitor 对象结构:
┌───────────────────────┐
│  Owner: 持有锁的线程    │
│  EntryList: 阻塞等待队列 │  ← 竞争锁失败的线程在此排队
│  WaitSet: wait等待队列   │  ← 调用wait()的线程在此等待
│  count: 重入次数         │
└───────────────────────┘

执行流程:
1. 线程进入synchronized → 尝试获取Monitor的Owner
2. 获取成功 → Owner=当前线程, count++
3. 获取失败 → 进入EntryList阻塞等待
4. 调用wait() → 释放Owner, 进入WaitSet
5. 被notify() → 从WaitSet移到EntryList, 重新竞争
6. 执行完毕 → count--, count=0时释放Owner
```

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

#### 为什么不用 Executors 内置线程池？（阿里规约 / 面试必问）

| 内置线程池 | 问题 | 风险 |
|-----------|------|------|
| `newFixedThreadPool(n)` | 队列用**LinkedBlockingQueue（无界）** | 任务堆积 → **OOM** |
| `newSingleThreadExecutor()` | 同上，无界队列 | 同上 → **OOM** |
| `newCachedThreadPool()` | maximumPoolSize = **Integer.MAX_VALUE** | 大量创建线程 → **OOM / CPU耗尽** |
| `newScheduledThreadPool(n)` | 队列用**DelayedWorkQueue（无界）** | 任务堆积 → **OOM** |

**面试话术**: "《阿里巴巴Java开发手册》强制要求不使用Executors创建线程池，因为FixedThreadPool和SingleThreadExecutor使用了无界队列LinkedBlockingQueue，可能导致大量任务堆积引发OOM；CachedThreadPool允许创建Integer.MAX_VALUE个线程，同样可能导致OOM。正确做法是通过ThreadPoolExecutor手动指定核心参数，特别是有界队列和合理的拒绝策略。"

```java
// 正确的线程池创建方式
ThreadPoolExecutor executor = new ThreadPoolExecutor(
    4,                                    // 核心线程数
    8,                                    // 最大线程数
    60, TimeUnit.SECONDS,                 // 空闲线程存活时间
    new LinkedBlockingQueue<>(1000),       // 有界队列！
    new ThreadFactoryBuilder().setNameFormat("biz-pool-%d").build(),  // 命名线程
    new ThreadPoolExecutor.CallerRunsPolicy()  // 拒绝策略
);
```

#### shutdown() vs shutdownNow()

| 方法 | 行为 | 返回值 |
|------|------|--------|
| `shutdown()` | **温和关闭**: 不接受新任务，但会执行完队列中的任务 | void |
| `shutdownNow()` | **强制关闭**: 尝试interrupt正在执行的线程，返回未执行的任务 | List<Runnable> |
| `awaitTermination()` | 阻塞等待所有任务完成（配合shutdown使用） | boolean |

**优雅关闭线程池的最佳实践**:
```java
executor.shutdown();                              // 1. 拒绝新任务
if (!executor.awaitTermination(60, SECONDS)) {    // 2. 等待60秒
    executor.shutdownNow();                        // 3. 超时强制关闭
    if (!executor.awaitTermination(60, SECONDS))   // 4. 再等60秒
        System.err.println("Pool did not terminate");
}
```

#### execute() vs submit()

| 方法 | 返回值 | 异常处理 | 参数 |
|------|--------|---------|------|
| `execute(Runnable)` | void | 直接抛出，线程终止 | 只接受Runnable |
| `submit(Callable/Runnable)` | **Future** | 异常封装在Future中，调用`get()`时抛出 | Callable或Runnable |

> **坑**: submit提交的任务如果不调用`Future.get()`，异常会被**静默吞掉**！

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

#### ThreadLocal 的 key 为什么是弱引用？（面试高频追问）

**如果key是强引用会怎样？**
```
假设 key 是强引用:
                    强引用                强引用
ThreadLocal 对象 ←──── Entry.key ←──── ThreadLocalMap (属于Thread)
       ↓
  用户代码中: threadLocal = null; // 想要回收ThreadLocal对象

结果: 即使用户将 threadLocal 变量置为null，
      ThreadLocalMap中的Entry.key仍然强引用着ThreadLocal对象
      → ThreadLocal对象永远无法被GC → 真正的内存泄漏（对象本身泄漏）
      而且: 连带value也无法回收
```

**用弱引用的好处**:
```
key 是弱引用:
                    弱引用                强引用
ThreadLocal 对象 ←──── Entry.key ←──── ThreadLocalMap
       ↓
  用户代码中: threadLocal = null; // 想要回收

结果: GC时，弱引用不阻止回收 → ThreadLocal对象被回收 → key变为null
      虽然value还在（短暂泄漏），但:
      1. 后续get/set/remove操作会清理key=null的Entry（探测式清理 + 启发式清理）
      2. 线程结束时ThreadLocalMap整体被回收
      → 相比强引用，泄漏风险从"必然泄漏"降为"可能短暂泄漏"
```

**总结一句话**: key 用弱引用是**两害相权取其轻** — 强引用会导致ThreadLocal对象本身无法回收（严重泄漏），弱引用只会导致value短暂泄漏（且有兜底清理机制）。

#### ThreadLocalMap 的清理机制

| 清理方式 | 触发时机 | 原理 |
|---------|---------|------|
| **探测式清理 (expungeStaleEntry)** | get/set/remove时遇到key=null的Entry | 从当前位置向后遍历，清理连续段内所有过期Entry |
| **启发式清理 (cleanSomeSlots)** | set新值后 | 对数级扫描 log2(n) 个位置，发现过期Entry则扩大扫描范围 |
| **全量清理 (expungeStaleEntries)** | rehash扩容时 | 遍历整个数组，清理所有过期Entry |

#### InheritableThreadLocal

- **ThreadLocal**: 父线程的值**不会**传递给子线程
- **InheritableThreadLocal**: 创建子线程时，会**拷贝**父线程的值到子线程
- **局限**: 线程池中线程是复用的，不会每次都创建子线程 → 值不会更新
- **解决**: 阿里巴巴的 **TransmittableThreadLocal (TTL)** — 在提交任务时自动传递上下文

```java
// 线程池中传递ThreadLocal的正确姿势（TTL）
ExecutorService executor = TtlExecutors.getTtlExecutorService(originalExecutor);
TransmittableThreadLocal<String> context = new TransmittableThreadLocal<>();
context.set("traceId-123");
executor.submit(() -> {
    System.out.println(context.get()); // 能正确获取 "traceId-123"
});
```

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

### 3.9 死锁（面试必会）

#### 死锁四个必要条件

1. **互斥**: 资源同一时刻只能被一个线程持有
2. **占有并等待**: 持有资源的线程在等待获取其他资源
3. **不可抢占**: 线程持有的资源不能被其他线程强制夺走
4. **循环等待**: 存在线程的循环等待链

#### 死锁代码示例

```java
// 经典死锁
Object lockA = new Object(), lockB = new Object();

// 线程1: 先拿A再拿B
new Thread(() -> {
    synchronized (lockA) {
        sleep(100);
        synchronized (lockB) { /* ... */ }
    }
}).start();

// 线程2: 先拿B再拿A
new Thread(() -> {
    synchronized (lockB) {
        sleep(100);
        synchronized (lockA) { /* ... */ }  // 死锁！
    }
}).start();
```

#### 死锁排查与解决

**排查**:
```
1. jstack <pid> 会直接输出 "Found one Java-level deadlock"
2. Arthas: thread -b  (直接定位阻塞线程)
3. VisualVM / JConsole 可视化检测
```

**预防策略**:
1. **破坏循环等待**: 对资源编号，按序获取（上例中两个线程都先lockA再lockB）
2. **破坏占有并等待**: 一次性获取所有资源
3. **超时机制**: `tryLock(timeout)` 超时放弃
4. **死锁检测**: 维护资源分配图，定期检测环

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

**为什么需要三级缓存而不是两级？（面试追问）**

```
两级缓存（成品 + 半成品）的问题:
  如果 A 被 AOP 代理了（需要创建代理对象），那么注入给B的应该是代理后的A，而非原始A。
  但是在创建A的早期阶段，我们还不确定A是否需要代理（正常流程是初始化后才AOP）。

三级缓存的解决方案:
  singletonFactories（三级）存放的是 ObjectFactory（lambda工厂函数）。
  当B需要注入A时，调用工厂函数 → 判断A是否需要代理：
    ├── 需要代理 → 提前创建代理对象返回
    └── 不需要 → 返回原始对象
  结果放入二级缓存（下次直接取，保证单例）。

核心: 三级缓存的作用是"延迟决策" — 只有在真正需要早期引用时，才决定返回原始对象还是代理对象。
```

#### @Autowired 注入流程

```
@Autowired 注入:
1. 按类型(byType)从容器中查找匹配的Bean
2. 找到多个 → 按字段名(byName)匹配
3. 还是多个 → 看有没有@Primary标注的
4. 还是多个 → 看有没有@Qualifier指定名称的
5. 都没有 → 报NoUniqueBeanDefinitionException
```

#### Spring 事务管理（面试高频）

**声明式事务核心注解**: `@Transactional`

| 属性 | 说明 | 默认值 |
|------|------|--------|
| `propagation` | 传播行为 | REQUIRED |
| `isolation` | 隔离级别 | DEFAULT（跟随数据库） |
| `timeout` | 超时时间(秒) | -1（不超时） |
| `readOnly` | 是否只读 | false |
| `rollbackFor` | 触发回滚的异常类型 | RuntimeException + Error |
| `noRollbackFor` | 不触发回滚的异常类型 | — |

**七种事务传播行为**:

| 传播行为 | 说明 | 常用度 |
|---------|------|:------:|
| **REQUIRED** | 有事务加入，无事务新建（**默认**） | ⭐⭐⭐ |
| **REQUIRES_NEW** | 无论如何新建事务（挂起当前事务） | ⭐⭐ |
| **NESTED** | 有事务则创建嵌套事务（savepoint），无事务新建 | ⭐⭐ |
| SUPPORTS | 有事务加入，无事务以非事务执行 | ⭐ |
| NOT_SUPPORTED | 以非事务执行（挂起当前事务） | ⭐ |
| MANDATORY | 必须在事务中调用，否则抛异常 | ⭐ |
| NEVER | 必须不在事务中，否则抛异常 | ⭐ |

**Spring 事务失效的 8 大场景（面试超高频）**:

| # | 失效场景 | 原因 | 解决方案 |
|---|---------|------|---------|
| 1 | **方法不是public** | Spring AOP通过代理调用，非public方法无法被代理拦截 | 改为public |
| 2 | **同类内部调用（self-invocation）** | `this.methodB()` 走的是原始对象而非代理对象 | 注入自身 / `AopContext.currentProxy()` |
| 3 | **异常被catch吞掉** | 事务靠异常触发回滚，catch后无异常 → 不回滚 | catch后重新throw / 手动`TransactionAspectSupport.currentTransactionStatus().setRollbackOnly()` |
| 4 | **抛出checked异常** | @Transactional默认只回滚RuntimeException和Error | 指定`rollbackFor = Exception.class` |
| 5 | **数据库引擎不支持** | MyISAM不支持事务 | 使用InnoDB |
| 6 | **Bean未被Spring管理** | 没有@Service/@Component等注解 | 确保Bean在容器中 |
| 7 | **多线程调用** | 事务基于ThreadLocal的Connection，新线程是新连接 | 不跨线程操作事务 / 编程式事务 |
| 8 | **propagation设为NOT_SUPPORTED** | 显式声明不使用事务 | 检查传播行为配置 |

**面试话术**: "Spring事务基于AOP代理实现，所以最常见的失效场景是同类方法内部调用——因为走的是this调用而非代理对象。其次是异常被catch了没有重新抛出，以及默认只回滚RuntimeException。我在项目中遇到过事务失效的问题，最终发现是service内部方法互调导致的，通过注入自身（self injection）解决了。"

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

#### Netty 粘包拆包

**产生原因**: TCP是字节流协议，没有消息边界的概念。发送方多个小包可能合并发送（Nagle算法），或一个大包被拆分多次发送。

| 解决方案 | 原理 | Netty实现 |
|---------|------|----------|
| **固定长度** | 每个消息固定N字节 | `FixedLengthFrameDecoder` |
| **分隔符** | 以特定字符分隔消息（如\n） | `DelimiterBasedFrameDecoder` |
| **长度字段** | 消息头中包含消息体长度 | `LengthFieldBasedFrameDecoder`（**最常用**） |
| **自定义协议** | 自定义编解码器 | 继承 `ByteToMessageDecoder` |

**项目关联**: 项目使用的gRPC底层基于HTTP/2，HTTP/2通过帧(Frame)机制天然解决了粘包拆包问题——每个帧有长度字段标识边界。

#### Netty 内存管理（高级）

| 概念 | 说明 |
|------|------|
| **PooledByteBufAllocator** | 池化内存分配器（默认），基于jemalloc算法，减少GC |
| **UnpooledByteBufAllocator** | 非池化，每次new分配 |
| **Arena** | 每个线程绑定一个Arena，减少锁竞争 |
| **PoolChunk** | 以16MB为单位管理内存，内部用完全二叉树(伙伴算法)分配 |
| **引用计数** | ByteBuf使用引用计数管理生命周期，`retain()/release()` |

> **常见坑**: ByteBuf忘记release导致**堆外内存泄漏**。Netty提供`ResourceLeakDetector`检测泄漏（开发时开启PARANOID级别）。

#### Netty 高性能设计总结

| 设计 | 说明 |
|------|------|
| **Reactor线程模型** | Boss线程接受连接 + Worker线程处理IO，避免无效线程 |
| **零拷贝** | CompositeByteBuf/FileRegion/DirectBuffer |
| **内存池** | PooledByteBufAllocator，减少GC和内存分配开销 |
| **串行无锁化** | 一个Channel绑定一个EventLoop，同一Channel的操作在同一线程执行 |
| **高效序列化** | 支持Protobuf等高效序列化框架 |
| **Pipeline机制** | Handler责任链，灵活组合编解码和业务逻辑 |

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

### 5.4 "手写一个阻塞队列"

```java
public class MyBlockingQueue<T> {
    private final Object[] items;
    private int count, putIndex, takeIndex;
    private final ReentrantLock lock = new ReentrantLock();
    private final Condition notFull = lock.newCondition();
    private final Condition notEmpty = lock.newCondition();
    
    public MyBlockingQueue(int capacity) {
        items = new Object[capacity];
    }
    
    public void put(T item) throws InterruptedException {
        lock.lock();
        try {
            while (count == items.length) notFull.await();  // 满了就等
            items[putIndex] = item;
            if (++putIndex == items.length) putIndex = 0;    // 环形数组
            count++;
            notEmpty.signal();                                // 通知消费者
        } finally {
            lock.unlock();
        }
    }
    
    @SuppressWarnings("unchecked")
    public T take() throws InterruptedException {
        lock.lock();
        try {
            while (count == 0) notEmpty.await();             // 空了就等
            T item = (T) items[takeIndex];
            items[takeIndex] = null;
            if (++takeIndex == items.length) takeIndex = 0;
            count--;
            notFull.signal();                                 // 通知生产者
            return item;
        } finally {
            lock.unlock();
        }
    }
}
```

> **要点**: 用Condition的`await/signal`实现精确唤醒（生产者只唤醒消费者，反之亦然），比wait/notifyAll更高效。

### 5.5 "手写LRU缓存"

```java
public class LRUCache<K, V> extends LinkedHashMap<K, V> {
    private final int capacity;
    
    public LRUCache(int capacity) {
        super(capacity, 0.75f, true);  // accessOrder=true 按访问顺序排列
        this.capacity = capacity;
    }
    
    @Override
    protected boolean removeEldestEntry(Map.Entry<K, V> eldest) {
        return size() > capacity;  // 超过容量时删除最久未访问的
    }
}

// 线程安全版: Collections.synchronizedMap(new LRUCache<>(100))
// 或者手动用 ReentrantReadWriteLock 包装
```

### 5.6 "生产者消费者模型的几种实现"

| 实现方式 | 核心API | 特点 |
|---------|---------|------|
| **synchronized + wait/notify** | Object.wait/notify | 最基础，不够灵活 |
| **ReentrantLock + Condition** | await/signal | 可精确唤醒，更灵活 |
| **BlockingQueue** | put/take | **最推荐**，代码最简洁 |
| **Semaphore** | acquire/release | 控制并发数 |
| **Disruptor** | RingBuffer | **最高性能**（无锁队列） |

### 5.7 "如何保证接口幂等性？"

| 方案 | 原理 | 适用场景 |
|------|------|---------|
| **唯一ID（Token）** | 请求前获取Token，处理时消费Token（Redis SETNX） | 表单重复提交 |
| **数据库唯一索引** | 利用唯一约束防止重复插入 | 创建类操作 |
| **乐观锁 (版本号)** | `UPDATE ... SET version=version+1 WHERE version=?` | 更新类操作 |
| **状态机** | 状态只能单向流转（如 待支付→已支付→已完成） | 状态变更操作 |
| **分布式锁** | 同一业务ID同一时刻只能一个请求处理 | 通用方案 |

### 5.8 "Java 对象到底占多少内存？"

```
以 64位 JVM + 压缩指针(默认开启) 为例:

空Object:
  对象头 = Mark Word(8B) + Klass Pointer(4B) = 12B
  + 对齐填充 = 4B
  = 16B（最小对象大小）

Integer:
  对象头 12B + int字段 4B = 16B

String (JDK8, 空字符串):
  对象头 12B + char[] ref(4B) + hash(4B) = 20B → 对齐 → 24B
  + char[]对象: 对象头 12B + length(4B) + 对齐 = 16B
  = 40B（一个空String就要40字节！这就是为什么大量小String很耗内存）

Boolean:
  对象头 12B + boolean字段 1B = 13B → 对齐 → 16B
  （一个boolean值包装成Boolean后，从1字节膨胀到16字节！）
```

**面试话术**: "一个空Object对象在64位JVM下占16字节（12字节对象头+4字节对齐），一个Integer占16字节，一个空String约40字节。这也是为什么在高性能场景中我们使用基本类型而非包装类型，项目中用JOL工具分析玩家数据对象大小，发现过度使用包装类型导致内存膨胀，优化后内存下降了30%。"

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