import { ref } from 'vue'

// 详情页在顶栏面包屑里显示的名称（如来源名）；离开详情页时清空，面包屑退回“详情”。
export const crumbTail = ref<string | null>(null)
