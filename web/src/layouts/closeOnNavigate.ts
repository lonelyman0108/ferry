import { defineComponent, watch } from 'vue'
import { useRoute } from 'vue-router'

import { useSidebar } from '@/components/ui/sidebar'

// 手机上侧边栏是抽屉，点了菜单跳转后要自己收起来。必须放在 SidebarProvider 里面才能拿到侧边栏状态。
export default defineComponent({
  setup() {
    const route = useRoute()
    const { setOpenMobile } = useSidebar()
    watch(
      () => route.path,
      () => setOpenMobile(false),
    )
    return () => null
  },
})
