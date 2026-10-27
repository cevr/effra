# #108 overflow-bench run 20261027T154229Z

```
date: 2026-10-27T15:42:29Z
head: e73a0d5f8a37684dc386e8137ed50f0e534ee6d5
package clean: false
go: go version go1.27.0 linux/amd64
go env GOAMD64 GOARCH GOOS: v1 amd64 linux
kernel: Linux 6.12.93 x86_64
cpu: AMD EPYC 9554P 64-Core Processor
logical cpus: 16
loadavg at start: 10.68 11.00 13.37 7/1835 3558390
```

## Host load (one-minute load average, sampled every 10 s)

| Phase | Samples | Median | Min | Max |
| --- | ---: | ---: | ---: | ---: |
| env | 2 | 10.68 | 10.68 | 10.68 |
| tests | 1 | 10.68 | 10.68 | 10.68 |
| count10 | 44 | 12.53 | 10.00 | 16.63 |
| interleaved | 45 | 10.81 | 5.62 | 12.65 |
| asm | 1 | 5.62 | 5.62 | 5.62 |

## Contiguous run (-count 10)

| Benchmark | n | Median ns/op | Min | Max | Median vs wrap | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AccountingSum/wrap | 10 | 843 | 621 | 956 | 1.00x | 0 | 0 |
| AccountingSum/defect-cmp | 10 | 4432 | 3418 | 5518 | 5.26x | 0 | 0 |
| AccountingSum/defect-post | 10 | 2690 | 2387 | 3199 | 3.19x | 0 | 0 |
| AccountingSum/defect-xor | 10 | 2341 | 2090 | 2612 | 2.78x | 0 | 0 |
| AccountingSum/defect-bits | 10 | 3302 | 2952 | 3504 | 3.92x | 0 | 0 |
| AccountingSum/defect-range | 10 | 1880 | 1781 | 2141 | 2.23x | 0 | 0 |
| AccountingSum/defect-range-shift | 10 | 2144 | 1921 | 2669 | 2.54x | 0 | 0 |
| AccountingSum/defect-range-outline | 10 | 2197 | 2001 | 2288 | 2.61x | 0 | 0 |
| AccountingSum/fail-cmp | 10 | 4763 | 4361 | 5215 | 5.65x | 0 | 0 |
| AccountingSum/fail-post | 10 | 3254 | 2686 | 3576 | 3.86x | 0 | 0 |
| AccountingSum/fail-xor | 10 | 3070 | 2763 | 3519 | 3.64x | 0 | 0 |
| AccountingSum/fail-bits | 10 | 3955 | 3787 | 4320 | 4.69x | 0 | 0 |
| AccountingSum/fail-range | 10 | 2866 | 2461 | 3681 | 3.40x | 0 | 0 |
| AccountingSum/fail-direct | 10 | 2094 | 1941 | 2355 | 2.48x | 0 | 0 |
| HashMix/short/wrap | 10 | 3016 | 2653 | 3223 | 1.00x | 0 | 0 |
| HashMix/short/explicit-wrap | 10 | 3160 | 2711 | 3265 | 1.05x | 0 | 0 |
| HashMix/short/defect-cmp | 10 | 5332 | 4342 | 5719 | 1.77x | 0 | 0 |
| HashMix/short/defect-bits | 10 | 6111 | 5643 | 6496 | 2.03x | 0 | 0 |
| HashMix/short/fail-cmp | 10 | 7131 | 6364 | 8934 | 2.36x | 0 | 0 |
| HashMix/short/fail-bits | 10 | 5762 | 5190 | 6442 | 1.91x | 0 | 0 |
| HashMix/long/wrap | 10 | 3628 | 3493 | 3963 | 1.00x | 0 | 0 |
| HashMix/long/explicit-wrap | 10 | 3516 | 3339 | 3793 | 0.97x | 0 | 0 |
| Op/add/wrap | 10 | 452 | 408 | 489 | 1.00x | 0 | 0 |
| Op/add/cmp | 10 | 2034 | 1861 | 2513 | 4.50x | 0 | 0 |
| Op/add/post | 10 | 913 | 782 | 1000 | 2.02x | 0 | 0 |
| Op/add/xor | 10 | 692 | 644 | 732 | 1.53x | 0 | 0 |
| Op/add/bits | 10 | 1084 | 1037 | 1136 | 2.40x | 0 | 0 |
| Op/sub/wrap | 10 | 448 | 389 | 480 | 1.00x | 0 | 0 |
| Op/sub/cmp | 10 | 1847 | 1574 | 1968 | 4.13x | 0 | 0 |
| Op/sub/post | 10 | 745 | 669 | 903 | 1.66x | 0 | 0 |
| Op/sub/xor | 10 | 539 | 491 | 568 | 1.20x | 0 | 0 |
| Op/sub/bits | 10 | 1082 | 1018 | 1152 | 2.42x | 0 | 0 |
| Op/mul/wrap | 10 | 521 | 484 | 540 | 1.00x | 0 | 0 |
| Op/mul/cmp | 10 | 2659 | 2457 | 2799 | 5.11x | 0 | 0 |
| Op/mul/bits | 10 | 1404 | 1238 | 1538 | 2.70x | 0 | 0 |
| Op/mul/range | 10 | 1216 | 1114 | 1308 | 2.34x | 0 | 0 |

Raw ns/op samples, in run order:

```
AccountingSum/wrap: 666 621 761 842 826 843 862 871 956 929
AccountingSum/defect-cmp: 4371 5518 4325 4690 4444 4391 3418 5223 5036 4421
AccountingSum/defect-post: 2734 2738 2657 2800 2387 2641 3199 2723 2567 2612
AccountingSum/defect-xor: 2464 2090 2360 2315 2429 2535 2274 2322 2612 2213
AccountingSum/defect-bits: 3234 2995 2952 3208 3434 3398 3227 3388 3504 3369
AccountingSum/defect-range: 1781 1821 1919 1988 1994 1851 2141 1809 1908 1830
AccountingSum/defect-range-shift: 1921 2127 1972 2491 1947 2208 2232 2669 2046 2160
AccountingSum/defect-range-outline: 2288 2230 2205 2079 2138 2230 2126 2189 2001 2205
AccountingSum/fail-cmp: 5005 4739 4779 5215 4361 4727 4583 5148 4813 4747
AccountingSum/fail-post: 3576 3062 3172 3346 3196 3313 3480 3346 2977 2686
AccountingSum/fail-xor: 3216 2763 2956 3519 3433 3178 2973 2881 3101 3039
AccountingSum/fail-bits: 4108 3840 3926 3787 4115 3790 3984 4320 4011 3849
AccountingSum/fail-range: 3681 2757 2589 2694 2975 3386 3073 2640 2461 3559
AccountingSum/fail-direct: 2355 2268 2123 1959 2177 2064 2211 1941 2018 1968
HashMix/short/wrap: 3109 2824 2980 3201 2653 3122 3019 3223 3012 2973
HashMix/short/explicit-wrap: 3198 2925 3216 3168 3151 3114 2900 3265 3190 2711
HashMix/short/defect-cmp: 5205 5719 5588 5464 5481 4726 5459 4716 4926 4342
HashMix/short/defect-bits: 6203 6078 6399 6496 6239 5978 6043 6144 5643 5861
HashMix/short/fail-cmp: 6712 7547 7145 6655 7117 7757 8934 6804 8256 6364
HashMix/short/fail-bits: 6442 5425 5975 5772 5707 6303 6329 5751 5190 5605
HashMix/long/wrap: 3618 3494 3639 3726 3918 3574 3594 3963 3648 3493
HashMix/long/explicit-wrap: 3793 3503 3339 3494 3650 3721 3452 3529 3493 3740
Op/add/wrap: 439 408 469 460 464 417 489 443 444 480
Op/add/cmp: 1897 2346 2352 1946 1981 1861 2513 2010 2057 2346
Op/add/post: 963 847 945 1000 986 912 890 782 915 835
Op/add/xor: 660 680 732 716 690 712 670 694 644 702
Op/add/bits: 1136 1063 1037 1118 1067 1102 1111 1063 1062 1109
Op/sub/wrap: 460 432 480 461 436 472 389 436 443 452
Op/sub/cmp: 1870 1687 1852 1574 1613 1943 1794 1968 1842 1946
Op/sub/post: 903 735 745 755 835 743 783 690 745 669
Op/sub/xor: 491 517 554 540 543 568 526 538 520 547
Op/sub/bits: 1064 1100 1152 1099 1077 1148 1087 1026 1077 1018
Op/mul/wrap: 516 533 540 507 508 540 532 525 484 505
Op/mul/cmp: 2707 2785 2638 2586 2457 2799 2680 2514 2560 2730
Op/mul/bits: 1538 1445 1392 1418 1238 1436 1305 1312 1361 1416
Op/mul/range: 1293 1119 1255 1308 1266 1215 1193 1186 1217 1114
```

## Interleaved run (one process per sample, shuffled each round)

| Benchmark | n | Median ns/op | Min | Max | Median vs wrap | Paired ratio median [min, max] | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| AccountingSum/wrap | 10 | 782 | 717 | 897 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| AccountingSum/defect-cmp | 10 | 4060 | 3087 | 5069 | 5.19x | 5.24x [3.56, 6.85] | 0 | 0 |
| AccountingSum/defect-post | 10 | 2556 | 2244 | 2854 | 3.27x | 3.26x [2.62, 3.92] | 0 | 0 |
| AccountingSum/defect-xor | 10 | 2336 | 1965 | 2662 | 2.99x | 2.95x [2.61, 3.71] | 0 | 0 |
| AccountingSum/defect-bits | 10 | 3323 | 2724 | 3521 | 4.25x | 4.06x [3.62, 4.77] | 0 | 0 |
| AccountingSum/defect-range | 10 | 1884 | 1565 | 2050 | 2.41x | 2.21x [2.08, 2.86] | 0 | 0 |
| AccountingSum/defect-range-shift | 10 | 1910 | 1632 | 2109 | 2.44x | 2.48x [1.90, 2.79] | 0 | 0 |
| AccountingSum/defect-range-outline | 10 | 2172 | 1727 | 2388 | 2.78x | 2.59x [2.30, 3.23] | 0 | 0 |
| AccountingSum/fail-cmp | 10 | 4165 | 3862 | 4727 | 5.32x | 5.29x [4.53, 5.77] | 0 | 0 |
| AccountingSum/fail-post | 10 | 2879 | 2612 | 3086 | 3.68x | 3.47x [3.34, 4.12] | 0 | 0 |
| AccountingSum/fail-xor | 10 | 2610 | 2360 | 2786 | 3.34x | 3.19x [2.71, 3.77] | 0 | 0 |
| AccountingSum/fail-bits | 10 | 3610 | 3063 | 4332 | 4.62x | 4.50x [4.02, 6.04] | 0 | 0 |
| AccountingSum/fail-range | 10 | 2380 | 1983 | 2490 | 3.04x | 2.81x [2.62, 3.47] | 0 | 0 |
| AccountingSum/fail-direct | 10 | 1835 | 1564 | 2007 | 2.35x | 2.30x [2.04, 2.54] | 0 | 0 |
| HashMix/short/wrap | 10 | 2548 | 2214 | 2886 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| HashMix/short/explicit-wrap | 10 | 2492 | 2417 | 2924 | 0.98x | 1.01x [0.87, 1.11] | 0 | 0 |
| HashMix/short/defect-cmp | 10 | 4620 | 3920 | 5551 | 1.81x | 1.77x [1.69, 2.22] | 0 | 0 |
| HashMix/short/defect-bits | 10 | 5678 | 5018 | 6442 | 2.23x | 2.19x [2.08, 2.42] | 0 | 0 |
| HashMix/short/fail-cmp | 10 | 6619 | 5449 | 8155 | 2.60x | 2.56x [2.35, 3.68] | 0 | 0 |
| HashMix/short/fail-bits | 10 | 5829 | 4557 | 6986 | 2.29x | 2.29x [1.97, 2.79] | 0 | 0 |
| HashMix/long/wrap | 10 | 3660 | 3511 | 3867 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| HashMix/long/explicit-wrap | 10 | 3574 | 3486 | 3989 | 0.98x | 0.98x [0.95, 1.05] | 0 | 0 |
| Op/add/wrap | 10 | 429 | 388 | 478 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| Op/add/cmp | 10 | 1930 | 1656 | 2252 | 4.50x | 4.28x [4.11, 5.36] | 0 | 0 |
| Op/add/post | 10 | 808 | 714 | 944 | 1.88x | 1.90x [1.66, 2.21] | 0 | 0 |
| Op/add/xor | 10 | 682 | 660 | 704 | 1.59x | 1.59x [1.39, 1.81] | 0 | 0 |
| Op/add/bits | 10 | 1063 | 1008 | 1250 | 2.48x | 2.58x [2.31, 2.64] | 0 | 0 |
| Op/sub/wrap | 10 | 451 | 398 | 644 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| Op/sub/cmp | 10 | 1881 | 1455 | 2178 | 4.17x | 4.12x [3.06, 5.13] | 0 | 0 |
| Op/sub/post | 10 | 805 | 700 | 1140 | 1.79x | 1.82x [1.40, 2.54] | 0 | 0 |
| Op/sub/xor | 10 | 552 | 469 | 654 | 1.22x | 1.22x [0.82, 1.52] | 0 | 0 |
| Op/sub/bits | 10 | 1072 | 975 | 1184 | 2.38x | 2.47x [1.68, 2.68] | 0 | 0 |
| Op/mul/wrap | 10 | 504 | 436 | 541 | 1.00x | 1.00x [1.00, 1.00] | 0 | 0 |
| Op/mul/cmp | 10 | 2454 | 2117 | 2866 | 4.87x | 4.93x [4.43, 5.83] | 0 | 0 |
| Op/mul/bits | 10 | 1445 | 1292 | 1541 | 2.87x | 2.89x [2.57, 3.05] | 0 | 0 |
| Op/mul/range | 10 | 1146 | 995 | 1336 | 2.27x | 2.25x [1.97, 2.74] | 0 | 0 |

Raw ns/op samples, in run order:

```
AccountingSum/wrap: 845 803 863 874 717 740 897 717 762 752
AccountingSum/defect-cmp: 4470 4614 4472 3115 4045 5069 3932 3419 4076 3087
AccountingSum/defect-post: 2719 2415 2854 2294 2528 2524 2691 2808 2244 2584
AccountingSum/defect-xor: 2226 2474 2324 2547 2662 2347 2519 2285 2272 1965
AccountingSum/defect-bits: 3346 3435 3300 3209 3420 3079 3375 3262 3521 2724
AccountingSum/defect-range: 1839 1740 1934 1890 2022 1878 1926 2050 1775 1565
AccountingSum/defect-range-shift: 1751 2105 2109 1659 1998 1900 1729 1993 1919 1632
AccountingSum/defect-range-outline: 2101 2144 2211 2217 2252 2388 2181 1872 2162 1727
AccountingSum/fail-cmp: 4395 4381 4727 4357 3978 4270 4060 3862 3962 3910
AccountingSum/fail-post: 2842 2815 2916 2922 2702 3048 3086 2931 2612 2795
AccountingSum/fail-xor: 2492 2527 2786 2724 2704 2642 2434 2579 2738 2360
AccountingSum/fail-bits: 3446 4072 3612 3690 3434 3562 3608 4332 3814 3063
AccountingSum/fail-range: 2444 2389 2263 2425 2490 1983 2372 2396 2035 2146
AccountingSum/fail-direct: 1726 1794 1996 2007 1722 1876 1944 1771 1883 1564
HashMix/short/wrap: 2503 2497 2886 2819 2214 2592 2664 2428 2595 2317
HashMix/short/explicit-wrap: 2593 2456 2729 2447 2454 2529 2924 2577 2417 2454
HashMix/short/defect-cmp: 4318 5551 5123 4966 4614 5133 4566 4627 4596 3920
HashMix/short/defect-bits: 6050 5695 6181 6442 5103 5706 5662 5313 5391 5018
HashMix/short/fail-cmp: 6520 5929 6821 7785 8155 6718 6292 6131 7280 5449
HashMix/short/fail-bits: 6986 5973 6181 5729 5437 5929 6243 5547 5662 4557
HashMix/long/wrap: 3549 3638 3684 3787 3737 3683 3511 3575 3867 3518
HashMix/long/explicit-wrap: 3488 3570 3578 3989 3830 3558 3583 3486 3658 3507
Op/add/wrap: 416 454 478 414 444 474 401 388 420 438
Op/add/cmp: 1916 1945 1989 1765 1828 2034 2122 1656 2252 1800
Op/add/post: 920 911 866 819 944 797 755 741 714 729
Op/add/xor: 683 677 700 678 695 660 704 702 680 667
Op/add/bits: 1020 1051 1240 1075 1111 1250 1030 1008 1093 1012
Op/sub/wrap: 476 430 472 644 399 448 475 398 454 412
Op/sub/cmp: 1916 1846 2065 1972 1677 1806 2178 2040 1455 1497
Op/sub/post: 798 812 869 900 834 1140 732 722 700 751
Op/sub/xor: 531 654 575 528 584 523 584 469 572 516
Op/sub/bits: 1021 1153 1138 1082 1005 1176 1021 1062 1184 975
Op/mul/wrap: 495 513 541 487 534 503 495 505 510 436
Op/mul/cmp: 2474 2358 2866 2837 2433 2766 2549 2426 2257 2117
Op/mul/bits: 1481 1478 1449 1407 1373 1441 1292 1541 1537 1318
Op/mul/range: 1081 1335 1137 1336 1154 1118 1276 995 1161 1043
```
