**Table of Contents**

- [编码能力高难测试任务（10题）](#toc-1)
  - [任务1：循环字符串子串检查（字符串旋转判定）arxiv.org](#toc-2)
  - [任务2：正则表达式匹配（支持.和*）geeksforgeeks.orggeeksforgeeks.org](#toc-3)
  - [任务3：括号补全生成（Dyck字母串）huggingface.co](#toc-4)
  - [任务4：N皇后问题求解（回溯搜索）](#toc-5)
  - [任务5：单词阶梯II（最短变换序列输出）](#toc-6)
  - [任务6：数独求解器（回溯搜索）](#toc-7)
  - [任务7：任务调度最小完成时间（子集搜索）](#toc-8)
  - [任务8：网络IP地址生成（跳过保留地址）](#toc-9)
  - [任务9：矩阵中的最长递增路径（DFS + 记忆化）](#toc-10)
  - [任务10：棋盘马步最短路径（图最短路径）](#toc-11)
- [复杂逻辑推理高难测试任务（10题）](#toc-12)
  - [任务11：代词消歧（Disambiguation QA）huggingface.co](#toc-13)
  - [任务12：形式逻辑推理（否定含蕴演绎）huggingface.co](#toc-14)
  - [任务13：颜色物体推理（Reasoning about Colored Objects）huggingface.co](#toc-15)
  - [任务14：逻辑顺序排布（Logical Deduction Puzzle）huggingface.co](#toc-16)
  - [任务15：蒙提霍尔问题变体（复杂概率推理）](#toc-17)
  - [任务16：ARC科学挑战题（多步常识推理）](#toc-18)
  - [任务17：事件时间顺序推理（Temporal Sequences）huggingface.co](#toc-19)
  - [任务18：洗牌跟踪（Tracking Shuffled Objects）huggingface.co](#toc-20)
  - [任务19：布尔函数语言问题（Web of Lies）huggingface.co](#toc-21)
  - [任务20：智力算术题（Multi-step Arithmetic）](#toc-22)

## 高分辨率LLM金丝雀测试集设计

为检测LLM模型是否被悄然替换为较弱版本，我们构建了一个20题的高分辨率“金丝雀”测试集，包括**10道编码能力**和**10道复杂逻辑推理**的高难度任务。每道题都针对GPT-4与GPT-3.5等模型性能差异显著的场景精心挑选，并提供明确的输入输出要求，以方便自动化评估模型表现差异[novelis.io](https://novelis.io/scientific-pub/gpt-3-5-vs-gpt-4-evaluating-chatgpts-reasoning-performance-in-zero-shot-learning/#:~:text=different%20reasoning%20tasks%20across%20eleven,performance%20of%20both%20models%20on)[alphaxiv.org](https://www.alphaxiv.org/benchmarks/university-of-washington/chain-of-thought-hub#:~:text=The%20benchmark%20demonstrates%20that%20GPT,art%20models)。下面按类别列出题目、能力考察点、选择理由，以及详细的提示（Prompt）设计与预期输出格式。

## 编码能力高难测试任务（10题）

针对需要复杂算法或精巧编程技巧的任务，选取HumanEval、MBPP、LeetCode Hard、BigCode Bench Hard等基准中的难题。这些题GPT-4往往能正确解决，而GPT-3.5或其他较弱模型明显力不从心，体现出高区分度[novelis.io](https://novelis.io/scientific-pub/gpt-3-5-vs-gpt-4-evaluating-chatgpts-reasoning-performance-in-zero-shot-learning/#:~:text=different%20reasoning%20tasks%20across%20eleven,performance%20of%20both%20models%20on)。每题提供函数签名、问题描述和单元测试，便于采用Pass@1或Pass@k指标自动评估正确性。

### 任务1：循环字符串子串检查（字符串旋转判定）[arxiv.org](https://arxiv.org/html/2407.21227v1#:~:text=1def%20cycpattern_check)

**能力要求**：字符串操作与算法巧思。需检查一个字符串的任意循环旋转是否是另一个字符串的子串。

**选择理由**：该问题源自HumanEval基准的难题示例（如`cycpattern_check`），需要巧用字符串重复等技巧[arxiv.org](https://arxiv.org/html/2407.21227v1#:~:text=1def%20cycpattern_check)。GPT-4往往能想到将待检查字符串拼接自身以简化判断，而GPT-3.5容易遗漏旋转情况或实现出错。

**任务描述**：实现函数`is_rotation_substring(s: str, t: str) -> bool`，判断字符串`t`或其任意循环旋转是否为字符串`s`的子串。返回`True`或`False`。

```text
pythondef is_rotation_substring(s: str, t: str) -> bool:
    """
    判断字符串 t 或它的任意循环旋转形式是否是字符串 s 的子串。
    如果是，返回 True；否则返回 False。
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonassert is_rotation_substring("abcdefg", "cdefgab") == True   # 循环右移
assert is_rotation_substring("abcdefg", "gfedcba") == False  # 非循环子串
assert is_rotation_substring("hello world", "worldhello ") == True  # 含空格的旋转
```

### 任务2：正则表达式匹配（支持.和\*）[geeksforgeeks.org](https://www.geeksforgeeks.org/dsa/implementing-regular-expression-matching/#:~:text=Given%20a%20text%20t%20and,test%20regular%20expression%20such%20that)[geeksforgeeks.org](https://www.geeksforgeeks.org/dsa/implementing-regular-expression-matching/#:~:text=Examples%3A)

**能力要求**：动态规划或递归回溯。实现一个基础正则引擎，支持通配符`.`（匹配任意单字符）和`*`（匹配前一个字符的零个或多个重复）。

**选择理由**：这是LeetCode Hard经典难题[geeksforgeeks.org](https://www.geeksforgeeks.org/dsa/implementing-regular-expression-matching/#:~:text=Given%20a%20text%20t%20and,test%20regular%20expression%20such%20that)。GPT-4能够正确处理`*`的复杂匹配逻辑，而GPT-3.5常因状态处理不全而失败，体现高区分度。

**任务描述**：实现函数`regex_match(s: str, p: str) -> bool`，判断文本串`s`是否匹配模式串`p`。模式`p`仅包含小写字母以及特殊字符`.`和`*`，其中`*`表示匹配前一个字符的任意次数（含0次）[geeksforgeeks.org](https://www.geeksforgeeks.org/dsa/implementing-regular-expression-matching/#:~:text=Given%20a%20text%20t%20and,test%20regular%20expression%20such%20that)。保证模式合法（每个`*`前面都有有效字符）。返回布尔值表示是否匹配。

```text
pythondef regex_match(s: str, p: str) -> bool:
    """
    实现简单的正则表达式匹配，支持通配符：
    '.' 匹配任意单一字符；
    '*' 匹配前一个元素零个或多个。
    返回 s 是否匹配模式 p。
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonassert regex_match("aaa", "a") == False        # "a" 无法匹配整个 "aaa":contentReference[oaicite:9]{index=9}
assert regex_match("abb", "a.*") == True       # "."匹配'b', "*"匹配一个前字符'b':contentReference[oaicite:10]{index=10}
assert regex_match("", "a*b*") == True         # '*' 可匹配零个字符:contentReference[oaicite:11]{index=11}
assert regex_match("abcd", ".*") == True       # ".*" 可匹配任意字符串
assert regex_match("abbb", "ab*") == True      # '*' 匹配多个前序字符
```

### 任务3：括号补全生成（Dyck字母串）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,its%20last%20few%20closing%20parentheses)

**能力要求**：递归回溯与栈操作。根据给定的部分括号序列，生成补全成有效括号字符串的缺失部分。

**选择理由**：括号匹配是算法典范难题之一，与Big Bench Hard中Dyck语言任务类似[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,its%20last%20few%20closing%20parentheses)。需要多层递归思考。GPT-4通常能正确生成所有有效补全，而GPT-3.5易遗漏或生成非法序列。

**任务描述**：实现函数`complete_brackets(prefix: str) -> str`，传入一个仅包含`(`和`)`的字符串前缀，其中括号可能不配对，返回可以补齐该前缀成为一个有效括号字符串的**最短**后缀字符串。例如：输入`"((()"`应输出`"))"`使整个字符串有效。若无法补全为有效序列则返回空字符串。

```text
pythondef complete_brackets(prefix: str) -> str:
    """
    给定括号序列前缀，返回补全其成为有效括号字符串的最短后缀。
    如果无法补全则返回空字符串。
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonassert complete_brackets("(()") == ")"            # "(" 需补一个 ")":contentReference[oaicite:14]{index=14}
assert complete_brackets("((()") == "))"          # "((" 需补两个 "))"
assert complete_brackets("()()") == ""            # 已经是有效括号串，无需补充
assert complete_brackets(")(") == ""              # 无法补全为有效序列
```

### 任务4：N皇后问题求解（回溯搜索）

**能力要求**：搜索与回溯算法。将N个皇后放置在N×N国际象棋棋盘上，任意两个皇后不在同一行、列或对角线。

**选择理由**：N皇后是经典回溯难题。GPT-4可快速编写正确的DFS解法，而GPT-3.5常漏检冲突或返回不完整解。该题要求输出一个可行解以供测试，因此对模型推理和代码正确性要求高。

**任务描述**：实现函数`solve_n_queens(n: int) -> List[List[str]]`，返回一个解的棋盘表示，将`n`个皇后（`Q`表示）放置在`n×n`棋盘上（空位用`.`表示）。输出是`n`个长度为`n`的字符串组成的列表，每字符串表示棋盘一行。

```text
pythondef solve_n_queens(n: int) -> List[List[str]]:
    """
    求解 n 皇后问题，返回任意一个符合条件的棋盘布局方案。
    棋盘用 'Q' 表示皇后，'.' 表示空位。
    若无解返回空列表。
    """
    # 在此实现函数
```

**单元测试**（以n=4为例）：

```text
pythonsolution = solve_n_queens(4)
assert solution != [] and len(solution) == 4
# 检验每行恰有一个皇后，每列也恰有一个皇后
for row in solution:
    assert row.count("Q") == 1
for c in range(4):
    assert sum(1 for row in solution if row[c] == "Q") == 1
# 检查对角线无冲突
coords = [(r, row.index("Q")) for r, row in enumerate(solution)]
for (r1, c1), (r2, c2) in itertools.combinations(coords, 2):
    assert abs(r1 - r2) != abs(c1 - c2)
```

### 任务5：单词阶梯II（最短变换序列输出）

**能力要求**：图算法与广度优先搜索（BFS），以及递归重建路径。找出从起始单词转换到目标单词的所有最短转换序列。

**选择理由**：LeetCode Hard“Word Ladder II”问题要求同时求出所有最短路径，涉及图搜索和路径回溯，难度远大于只求最短距离。GPT-4较能正确实现多路径回溯输出，而GPT-3.5常遗漏部分解或算法效率不佳。

**任务描述**：实现函数`find_ladders(begin: str, end: str, word_list: List[str]) -> List[List[str]]`，返回从`begin`单词到`end`单词的所有最短转换序列。转换规则：每次改变一个字母，且变换后单词必须在`word_list`中。输出按任意顺序给出所有序列。

```text
pythondef find_ladders(begin: str, end: str, word_list: List[str]) -> List[List[str]]:
    """
    在 word_list 单词表中，找到从 begin 到 end 的所有最短单词转换序列。
    每次转换只能改变一个字母，且中间单词必须在 word_list 中。
    返回所有最短路径（任一路径中包含 begin 和 end）。
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonword_list = ["hot","dot","dog","lot","log","cog"]
res = find_ladders("hit", "cog", word_list)
# 所有序列应该长度为5（包括起点和终点），如：
expected_solutions = [
    ["hit","hot","dot","dog","cog"],
    ["hit","hot","lot","log","cog"]
]
assert all(path in expected_solutions for path in res) and len(res) == len(expected_solutions)
```

### 任务6：数独求解器（回溯搜索）

**能力要求**：深度回溯与剪枝。在9x9数独棋盘上填入数字，使其符合行、列、九宫格均含1-9且不重复的规则。

**选择理由**：数独求解需要系统的回溯和高效剪枝。GPT-4可以正确实现回溯搜索并找到解，而GPT-3.5常出现部分填错或死循环。通过提供具体棋盘测试，可严谨检验模型代码是否真正完成搜索。

**任务描述**：实现函数`solve_sudoku(board: List[List[int]]) -> List[List[int]]`，将输入的9x9未完成数独棋盘（用0表示空格）求解，返回解后的棋盘（如果有解假定唯一）。输出也是9x9的整数矩阵。

```text
pythondef solve_sudoku(board: List[List[int]]) -> List[List[int]]:
    """
    求解给定的9x9数独谜题，返回解后的棋盘。
    输入board中的0表示空白。
    保证输入数独至少有一种解。
    """
    # 在此实现函数
```

**单元测试**（提供一个中等难度数独局面及其唯一解）：

```text
pythonpuzzle = [
    [5,3,0, 0,7,0, 0,0,0],
    [6,0,0, 1,9,5, 0,0,0],
    [0,9,8, 0,0,0, 0,6,0],
    [8,0,0, 0,6,0, 0,0,3],
    [4,0,0, 8,0,3, 0,0,1],
    [7,0,0, 0,2,0, 0,0,6],
    [0,6,0, 0,0,0, 2,8,0],
    [0,0,0, 4,1,9, 0,0,5],
    [0,0,0, 0,8,0, 0,7,9],
]
solution = solve_sudoku([row[:] for row in puzzle])
# 验证解的每行每列每九宫格均为1-9不重复
for i in range(9):
    assert sorted(solution[i]) == list(range(1,10))
    assert sorted(solution[r][i] for r in range(9)) == list(range(1,10))
for br in range(3):
    for bc in range(3):
        block = [solution[r][c] for r in range(br*3, br*3+3) for c in range(bc*3, bc*3+3)]
        assert sorted(block) == list(range(1,10))
```

### 任务7：任务调度最小完成时间（子集搜索）

**能力要求**：组合搜索与剪枝优化。将若干作业分配给K个工人，求使所有作业完成的最短时间。

**选择理由**：该问题相当于NP难的调度优化，需要搜索所有可能分配并剪枝（类似LeetCode 1723“完成所有工作的最短时间”）[leetcode.com](https://leetcode.com/problems/find-minimum-time-to-finish-all-jobs/#:~:text=1723,you%20can%20assign%20jobs%20to)。GPT-4能比较好地实现递归+剪枝找出最优，而GPT-3.5容易遗漏剪枝导致超时或出错。

**任务描述**：实现函数`min_time_to_finish(jobs: List[int], k: int) -> int`，将`jobs`列表中的每个作业（以耗时表示）分配给`k`个工人，要求所有工人并行完成所有作业，返回最短可能完成时间。假设`k <= len(jobs)`。

```text
pythondef min_time_to_finish(jobs: List[int], k: int) -> int:
    """
    将每项作业（jobs中的数字表示时长）分给k个工人，求使所有作业完成的最小总时间。
    返回这个最小完成时间。
    """
    # 在此实现函数
```

**单元测试**：

```text
python# 示例：3个工人分配6项作业
jobs = [3, 1, 3, 2, 4, 5]
# 最优分配：如 [3,5]、[4,2]、[3,1]，各组时间 8,6,4 -> 完成时间8
assert min_time_to_finish(jobs, 3) == 8

jobs2 = [2,2,3,7,1,5,2]
# 2个工人，最优可将任务分成两组时间分别为11和11
assert min_time_to_finish(jobs2, 2) == 11
```

### 任务8：网络IP地址生成（跳过保留地址）

**能力要求**：字符串解析与边界条件处理。根据网络地址和子网掩码，列出该子网中所有可用的主机IP，不包括网络地址和广播地址。

**选择理由**：此任务取自BigCode Bench Hard子集中网络运维领域的一道难题（如BCB-Hard/147）[arxiv.org](https://arxiv.org/html/2511.04355v1#:~:text=BCB,optimize%20for%20general)。需要对IP和掩码进行计算，涉及边界案例。GPT-4可正确跳过网络/广播地址生成清单，而GPT-3.5往往缺乏对IP计算的准确理解[arxiv.org](https://arxiv.org/html/2511.04355v1#:~:text=BCB,to%2077)。

**任务描述**：实现函数`list_hosts(network: str, mask: int) -> List[str]`，给定网络地址（如“192.168.1.0”）和CIDR前缀长度掩码（如24），返回该子网中所有可用主机IP地址的列表（字符串形式）。不包括网络地址本身和广播地址。

```text
pythondef list_hosts(network: str, mask: int) -> List[str]:
    """
    给定网络地址和CIDR前缀长度mask，返回该子网内所有可用主机IP地址列表。
    （排除子网网络地址和广播地址）
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonhosts = list_hosts("192.168.1.0", 30)  # 192.168.1.0/30 子网，可用主机地址应为 192.168.1.1 和 192.168.1.2
assert hosts == ["192.168.1.1", "192.168.1.2"]

hosts = list_hosts("10.0.0.0", 29)  # 10.0.0.0/29，可用主机 10.0.0.1 - 10.0.0.6
assert hosts[0] == "10.0.0.1" and hosts[-1] == "10.0.0.6" and len(hosts) == 6
```

### 任务9：矩阵中的最长递增路径（DFS + 记忆化）

**能力要求**：图形/矩阵遍历与记忆化递归。找到矩阵中严格递增的最长路径长度（任意相邻上下左右移动）。

**选择理由**：此LeetCode Hard问题要求对每个位置进行DFS并带记忆化防止重复计算。GPT-4倾向于正确实现DFS+缓存，而GPT-3.5可能遗漏记忆导致超时。该题验证模型对递归优化的掌握。

**任务描述**：实现函数`longest_increasing_path(matrix: List[List[int]]) -> int`，计算给定二维整数矩阵中严格递增路径的最大长度。路径可以从任意格子开始，每步移动至上下左右邻格，且要求后续值大于当前值。

```text
pythondef longest_increasing_path(matrix: List[List[int]]) -> int:
    """
    给定二维整数矩阵，返回其中严格递增路径的最长长度。
    路径可从任意起点，移动方向为上/下/左/右，相邻且后值大于前值。
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonmat1 = [
    [9, 9, 4],
    [6, 6, 8],
    [2, 1, 1]
]
# 最长递增路径例如 1->2->6->9 长度4:contentReference[oaicite:18]{index=18}
assert longest_increasing_path(mat1) == 4

mat2 = [
    [3,4,5],
    [3,2,6],
    [2,2,1]
]
# 最长路径 3->4->5->6 长度4
assert longest_increasing_path(mat2) == 4
```

### 任务10：棋盘马步最短路径（图最短路径）

**能力要求**：广度优先搜索（BFS）。在棋盘上计算马（“日”字走法）从起点跳到终点的最小步数。

**选择理由**：这是经典图搜索难题，可抽象为棋盘坐标上的最短路径。GPT-4能正确实现BFS拓展坐标并避免走回头，而GPT-3.5有时会遗漏访问状态或计算步数出错。这道题检验模型处理状态空间和最短路径的正确性。

**任务描述**：实现函数`knight_min_moves(n: int, start: Tuple[int,int], end: Tuple[int,int]) -> int`，计算在`n x n`棋盘上，国际象棋中的骑士（马）从起点坐标`start`跳到终点`end`所需的最少步数。棋盘坐标以(行,列)表示，0-indexed。如果无法到达返回-1。

```text
pythondef knight_min_moves(n: int, start: Tuple[int,int], end: Tuple[int,int]) -> int:
    """
    计算 n×n 棋盘上骑士从起点跳到终点的最小移动步数。
    start 和 end 是 (row, col) 格式的坐标。
    若无法到达则返回 -1。
    """
    # 在此实现函数
```

**单元测试**：

```text
pythonassert knight_min_moves(8, (0,0), (7,7)) == 6   # 标准棋盘一个对角位置，至少6步
assert knight_min_moves(8, (0,0), (0,0)) == 0   # 起终点相同
assert knight_min_moves(5, (0,0), (4,4)) == 4   # 5x5棋盘从角落到对角
```

以上编码任务涵盖字符串、动态规划、搜索、图算法等多方面，均为复杂度较高的问题。GPT-4在这些任务上的正确率显著高于GPT-3.5[novelis.io](https://novelis.io/scientific-pub/gpt-3-5-vs-gpt-4-evaluating-chatgpts-reasoning-performance-in-zero-shot-learning/#:~:text=different%20reasoning%20tasks%20across%20eleven,performance%20of%20both%20models%20on)。通过自动运行单元测试，可严格判定模型是否退化。

## 复杂逻辑推理高难测试任务（10题）

针对需要多步骤链式思考（Chain-of-Thought, CoT）的推理难题，我们挑选FrontierMath、LogiQA、Big-Bench Hard (BBH)、ARC Challenge等数据集中具有区分度的题目[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=fruits%29)[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,final%20positions%20of%20the%20objects)。这些题目通常需要模型逐步推理才能得出正确结论，GPT-4能够在逐步推理后回答正确，而GPT-3.5等较弱模型往往中途出错。我们为每题制定了明确的**问题说明**、要求模型输出**思维链**以及**预期输出格式**（如JSON），以方便比对答案的正确性，实现稳定的自动评估。

> **注**：在下列 Prompt 中，我们要求模型显式输出推理过程，然后给出最终答案的JSON格式。这不仅鼓励模型使用链式思考，也使得评估者能够严格检查推理步骤以及最终结论是否正确[novelis.io](https://novelis.io/scientific-pub/gpt-3-5-vs-gpt-4-evaluating-chatgpts-reasoning-performance-in-zero-shot-learning/#:~:text=different%20reasoning%20tasks%20across%20eleven,performance%20of%20both%20models%20on)。自动评估时将主要依据最后给出的JSON答案字段。

### 任务11：代词消歧（Disambiguation QA）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=answer%20the%20provided%20question)

**问题说明**：给出一个包含指代代词的句子，判断该句子是否存在指代歧义。如果不歧义，指出代词所指代的名词；如果歧义（即根据句子无法确定代词指谁），输出一个特定标志。

**Chain-of-Thought 要求**：模型应首先分析句子结构，找出代词及可能的先行词，判断歧义性，给出推理依据。逐步推理明确：代词是否有唯一明确的指代对象。

**预期输出格式**：如果不歧义，输出`{"antecedent": "<代词指代的名词>"}`；如果存在歧义无法确定，输出`{"antecedent": null}`。

**示例**：

用户提示：

```text
句子：小李告诉小王，他需要多练习篮球。
请判断句中“他”是否歧义，并给出指代。
```

模型预期思维链与回答：

```text
csharp思考：句中出现“小李”和“小王”两个男性名词，“他”可以指代“小李”或“小王”，句意无法确定指谁，因此存在歧义。
结论：指代不明确。
{"antecedent": null}
```

测试：对于**明确指代**的情况，模型应输出具体名词。例如句子“琴把球递给了丽莎，因为她需要用它练习投篮。”代词“她”明确指代“丽莎”，应输出`{"antecedent": "丽莎"}`。

### 任务12：形式逻辑推理（否定含蕴演绎）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,valid%20arguments%20from%20formal%20fallacies)

**问题说明**：给出一组前提陈述和一个结论陈述，涉及逻辑演绎和否定。判断结论在逻辑上是否能够从前提推出（有效推理），或者是形式谬误。

**Chain-of-Thought 要求**：模型应逐条分析前提，运用逻辑规则（尤其是否涉及**否定**的谬误），判断结论是否成立。需要清晰列出推理步骤。

**预期输出格式**：输出`{"valid": true}`如果结论可由前提严格推得，否则输出`{"valid": false}`。

**示例**：

用户提示：

```text
markdown前提：
1. 如果张三不抽烟，那么李四喝酒。
2. 李四没有喝酒。
结论：
张三抽烟。
以上结论在逻辑上是否成立？
```

模型预期推理与回答：

```text
rust思考：前提1是“¬抽烟 -> 喝酒”，其逆否命题为“¬喝酒 -> 抽烟”。前提2给出“¬喝酒”为真。根据逆否命题，可推得“抽烟”为真。
结论：“张三抽烟”可以从前提逻辑推出，推理有效。
{"valid": true}
```

如果结论为谬误，模型应解释是哪一步不支持，并输出`{"valid": false}`。例如若结论与前提无关，则应判定为false。

### 任务13：颜色物体推理（Reasoning about Colored Objects）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=information,the%20attributes%20of%20the%20penguins)

**问题说明**：给出若干关于带颜色物体相对位置的描述，然后问一个关于某物体颜色的简单问题。

**Chain-of-Thought 要求**：模型应将描述逐句转化为对物体位置和颜色的理解，构建一个场景心智模型。逐步推理问题所问物体的颜色。

**预期输出格式**：`{"color": "<颜色名称>"}`。

**示例**（改编自BBH任务[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=information,the%20attributes%20of%20the%20penguins)）：

用户提示：

```text
桌子上有一个红色的方块和一个蓝色的球。蓝色的球在红色的方块上面。问：方块是什么颜色？
```

模型推理与回答：

```text
css思考：有红色方块，蓝色球在红方块上。问题问方块颜色，很明显方块是红色。
{"color": "红色"}
```

若场景更复杂，模型应理清关系后作答。例如：“**一个绿色圆柱在蓝色立方体的左边，蓝色立方体在桌子正中**。问：圆柱的颜色？”模型应正确输出`{"color": "绿色"}`，并在思维链中体现对空间关系的理解。

### 任务14：逻辑顺序排布（Logical Deduction Puzzle）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,their%20spacial%20relationships%20and%20placements)

**问题说明**：提供一组关于若干对象顺序或排列的线索（例如位置、顺序、关系等），要求推理出各对象的具体顺序。

**Chain-of-Thought 要求**：模型需将每条线索翻译成约束条件，然后逐步消除不可能情况，得出唯一顺序。完整列出推理过程中的约束应用和更新。

**预期输出格式**：以JSON输出最终排序结果，例如`{"order": ["对象A", "对象B", ..., "对象N"]}`（按要求顺序列出）。

**示例**：

用户提示：

```text
diff有甲、乙、丙三个盒子从左到右排成一行。已知：
- 红球在甲盒子右边。
- 乙盒子里不是红球。
- 蓝球在最右边的盒子。
请问每个盒子里的球颜色分别是什么？
```

模型推理与回答：

```text
arduino思考：共有3个盒子甲乙丙；线索1：红球在甲右边，故红球不在甲，可能在乙或丙。
线索2：乙不装红球，因此红球不在乙，只能在丙。
线索3：蓝球在最右边盒子，即丙盒子为蓝球。
但红球已确定在丙，与蓝球冲突，因为一个盒子只能有一种球色。
检查矛盾：可能理解错。线索3蓝球在最右边，若丙蓝球，则红球无法放置——出现矛盾，尝试另一种满足线索1的方法：
若红球在甲右边，可以在乙或丙，但因矛盾推理，重新审视... (模型应正确推理避免矛盾)
(假定模型经过正确推理后) 得出最终：甲=绿球, 乙=红球, 丙=蓝球。
{"order": ["绿球", "红球", "蓝球"]}
```

_(以上示例推理略简，模型应详细列出消除矛盾过程。)_

本题要求模型在链式推理中清晰演绎约束**且不犯矛盾**。GPT-4擅长此类多步骤约束推理，而GPT-3.5常在复杂约束下混淆[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,their%20spacial%20relationships%20and%20placements)。

### 任务15：蒙提霍尔问题变体（复杂概率推理）

**问题说明**：给出蒙提霍尔三门问题或其变体，要求模型分析在给定策略下获胜的概率，或做出最优策略判断。

**Chain-of-Thought 要求**：模型应回顾蒙提霍尔问题经典解法，逐步分析每种选择情况的概率，展示清晰的条件概率计算过程。

**预期输出格式**：如果要求概率，输出形如`{"probability": 0.67}`（概率值保留两位小数）；如果要求策略，输出\`{"best_strategy": "<描述>"}。（注意将答案置于JSON中便于比对。）

**示例**：

用户提示：

```text
蒙提霍尔问题：有三扇门，其中一扇后有奖品，主持人知道奖品所在。你选了一扇门，主持人打开另一扇空门并询问你是否要换门。问：换门策略胜率是多少？
```

模型推理与回答：

```text
swift思考：经典蒙提霍尔，换门获胜概率为2/3。不换是1/3。
步骤：初选正确概率1/3；初选错概率2/3，主持人打开另一错门，此时换门则赢。故换门赢率2/3。
{"probability": 0.67}
```

如果问题问最优策略应回答`{"best_strategy": "换门"}`。要求模型通过链式分析得出正确概率结论。此类概率题GPT-4较稳定算对，而GPT-3.5易受直觉误导答错概率。

### 任务16：ARC科学挑战题（多步常识推理）

**问题说明**：选自AI2 ARC挑战赛科学题。例如一个需要结合多条常识才能得出正确答案的多项选择题。

**Chain-of-Thought 要求**：模型应列出所需的每条科学常识，并逐步将它们与题干条件结合，排除干扰选项，最终选择正确答案。

**预期输出格式**：输出JSON，注明`{"answer": "<选项>"}（例如"A"或"B"等)`。

**示例**（虚构ARC风格题）：

用户提示：

```text
css问题：植物在光下制造糖分的过程称为什么？
A. 光合作用  B. 蒸腾作用  C. 呼吸作用  D. 发酵作用
请给出答案并解释原因。
```

模型推理与回答：

```text
css思考：植物在光下制造糖，即将二氧化碳和水转化为葡萄糖并释放氧气，是光合作用。这是基本常识。
排除：蒸腾是水分蒸发，不对；呼吸作用是消耗养分；发酵是无氧条件微生物过程。
正确答案为A。
{"answer": "A"}
```

对于更复杂的ARC题，模型应列出多条知识点。例如涉及光、热、物质状态等多个概念的题目。GPT-4在综合常识推理上正确率更高，而GPT-3.5易遗漏关键知识导致选错。

### 任务17：事件时间顺序推理（Temporal Sequences）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,free%20to%20perform%20another%20activity)

**问题说明**：给出一个人一天中完成的若干事件和时间线索，询问他在某个特定时段可能在做什么（或有空闲的时间段）。

**Chain-of-Thought 要求**：模型应将一天的事件排序或映射到时间轴上，结合线索推断特定时间段的状态（忙碌或空闲，从事何事）。需要清晰列出时间推理过程。

**预期输出格式**：如果问题问该人在某时间是否有空闲，则输出`{"free": true}`或`false`；如果问在做什么，则输出`{"activity": "<活动>"}`。

**示例**（改编自BBH Temporal Sequences[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,free%20to%20perform%20another%20activity)）：

用户提示：

```text
diff小明的日程：
- 早上9点到10点上数学课
- 下午2点到4点打篮球
问：小明在上午10点半有没有空？
```

模型推理与回答：

```text
makefile思考：列出日程：
9:00-10:00 数学课
14:00-16:00 篮球
上午10:30 falls between 10:00 and 14:00 with no scheduled activity.
因此10:30小明是空闲的。
{"free": true}
```

如果时间段冲突，应输出false。本题考查模型处理时间线及间隔的能力。GPT-4对明确列出的时间安排推理准确，而GPT-3.5可能在跨越中午等情境下弄错时段。

### 任务18：洗牌跟踪（Tracking Shuffled Objects）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,final%20positions%20of%20the%20objects)

**问题说明**：有若干物体的初始位置，通过一系列两两交换操作（洗牌指令），最后询问特定物体的位置或某位置上的物体。

**Chain-of-Thought 要求**：模型应逐步模拟每一步交换操作，追踪目标物体的位置变化（或全局位置变化）。建议明确记录每一步交换前后的位置关系，最终得到答案。

**预期输出格式**：若问“物体X最终位置”，输出`{"position": <位置索引>}`；若问“位置Y上的物体”，输出`{"object": "<名称>"}`。

**示例**：

用户提示：

```text
scss初始位置：[A, B, C, D]（索引0-3）
操作：swap(0, 2), swap(1, 3), swap(0, 1)
问：最终索引0处是哪个物体？
```

模型推理与回答：

```text
mathematica思考：初始: idx0=A, idx1=B, idx2=C, idx3=D
执行swap(0,2): 位置0<->2，序列变为[C, B, A, D]
执行swap(1,3): 位置1<->3，序列变为[C, D, A, B]
执行swap(0,1): 位置0<->1，序列变为[D, C, A, B]
最终索引0处为物体D。
{"object": "D"}
```

如问物体A的位置则输出`{"position": 2}`（根据上述最终结果A在索引2）。本题需要模型严格执行每步，GPT-4通常能正确跟踪，而GPT-3.5有时会在多次交换后混淆[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,final%20positions%20of%20the%20objects)。

### 任务19：布尔函数语言问题（Web of Lies）[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,language%20word%20problem)

**问题说明**：给出一个自然语言描述的布尔表达式问题，让模型判断该表达式的真值。例如题干可能描述一个由多个条件通过“且/或/非”组成的复杂陈述，并给出某种输入情形，问最终结果真假。

**Chain-of-Thought 要求**：模型应把自然语言翻译成逻辑表达式，逐步计算子表达式的值。清晰列出每个子公式的真值计算过程。

**预期输出格式**：`{"result": true}`或`false`。

**示例**：

用户提示：

```text
条件描述：如果天空下雨且我带伞，那么我不会被淋湿；如果天空下雨但我没带伞，那么我会被淋湿；如果不下雨，无论是否带伞都不会被淋湿。
现在假设：天空在下雨，我带了伞。问：我会被淋湿吗？
```

模型推理与回答：

```text
rust思考：翻译逻辑：
R=下雨, U=淋湿, B=带伞
规则1: R ∧ B -> ¬U
规则2: R ∧ ¬B -> U
规则3: ¬R -> ¬U
现况：R=true, B=true
套用规则1：因为R且B为真，推出¬U，即U=false。
{"result": false}
```

模型应该给出`false`表示不会被淋湿。本题考验模型将自然语言转换成布尔逻辑的能力。GPT-4通常能够正确翻译并推理，而GPT-3.5可能遗漏隐含条件或推理出错[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=,language%20word%20problem)。

### 任务20：智力算术题（Multi-step Arithmetic）

**问题说明**：给出一道复杂的文字算术题，涉及多步计算（例如单价累计、找零、行程规划中的距离计算等）。

**Chain-of-Thought 要求**：模型应逐步列出计算过程，每一步标明计算公式或数值变化，确保最终答案正确无误。

**预期输出格式**：如果问具体数值，输出形如`{"answer": 42}`；如果问选择题，则输出对应选项字母的JSON。

**示例**：

用户提示：

```text
小明买了3支铅笔和2本笔记本，铅笔单价2元，笔记本单价5元，他给收银员20元，应找回多少元？
```

模型推理与回答：

```text
arduino思考：计算总价：铅笔3*2=6元，笔记本2*5=10元，总计16元。
付款20元，找零=20-16=4元。
{"answer": 4}
```

模型应输出找零4元的JSON。在更复杂情况下（例如多段行程距离汇总），GPT-4通过逐步算可避免出错，而GPT-3.5在多步计算中较易出错或遗漏步骤。通过要求列出思维链，评估者可检查每步计算的正确性，保证最终答案可靠。

---

以上20道**金丝雀测试**题覆盖编码和推理两大类别艰难场景。每题都包含对模型能力的细粒度考查，以及明确的输出格式，确保结果易于自动判分。当LLM模型性能出现退化时，这些题目将首先“亮红灯”——正如业内所称，它们充当了检测模型行为漂移的**金丝雀提示集**[medium.com](https://medium.com/@mike.besso/it-worked-yesterday-ed7285d480bb#:~:text=Why%20Prompt%20Regression%20Testing%20Is,in%20the%20AI%20Coal%20Mine)。我们参考已有工作中的设计原则：挑选**边缘难度**任务、要求**确定性的输出**、以及在持续评估中**监控固定的一组探针**[mobidev.biz](https://mobidev.biz/blog/llm-security-guide-for-ctos-it-security-officers#:~:text=LLM%20Security%3A%20Complete%20Guide%20for,Pair)[openagentschool.org](https://www.openagentschool.org/ai-skills#:~:text=Applied%20AI%20Skills%20Explorer%20,Guardrails)。通过定期运行这些高分辨率测试集题目并比较模型答案与预期，我们可以敏锐地捕捉模型性能的细微变化，及时发现是否模型被替换或质量下降，从而保障LLM部署的稳定可靠[medium.com](https://medium.com/@mike.besso/it-worked-yesterday-ed7285d480bb#:~:text=Why%20Prompt%20Regression%20Testing%20Is,in%20the%20AI%20Coal%20Mine)。

**参考来源：**

- GPT-4相较GPT-3.5在复杂任务上的优势[novelis.io](https://novelis.io/scientific-pub/gpt-3-5-vs-gpt-4-evaluating-chatgpts-reasoning-performance-in-zero-shot-learning/#:~:text=different%20reasoning%20tasks%20across%20eleven,performance%20of%20both%20models%20on)[alphaxiv.org](https://www.alphaxiv.org/benchmarks/university-of-washington/chain-of-thought-hub#:~:text=The%20benchmark%20demonstrates%20that%20GPT,art%20models)
- Big-Bench Hard任务列表及描述[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=answer%20the%20provided%20question)[huggingface.co](https://huggingface.co/datasets/maveriq/bigbenchhard#:~:text=information,the%20attributes%20of%20the%20penguins)
- HumanEval/MBPP/LeetCode Hard编码任务示例[arxiv.org](https://arxiv.org/html/2407.21227v1#:~:text=1def%20cycpattern_check)[geeksforgeeks.org](https://www.geeksforgeeks.org/dsa/implementing-regular-expression-matching/#:~:text=%3E%20%60Input%3A%60%60%20t%20%3D%20,character%20hence%20p%20becomes%20abb)
- BigCode Bench Hard任务难度分析[arxiv.org](https://arxiv.org/html/2511.04355v1#:~:text=BCB,to%2077)[arxiv.org](https://arxiv.org/html/2511.04355v1#:~:text=BCB,optimize%20for%20general)
- 模型回归测试与金丝雀提示的实践建议[medium.com](https://medium.com/@mike.besso/it-worked-yesterday-ed7285d480bb#:~:text=Why%20Prompt%20Regression%20Testing%20Is,in%20the%20AI%20Coal%20Mine)[mobidev.biz](https://mobidev.biz/blog/llm-security-guide-for-ctos-it-security-officers#:~:text=LLM%20Security%3A%20Complete%20Guide%20for,Pair)
