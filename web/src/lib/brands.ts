import bark from '@/assets/brands/bark.png'
import egern from '@/assets/brands/egern.png'
import loon from '@/assets/brands/loon.png'
import qinglong from '@/assets/brands/qinglong.svg'
import qx from '@/assets/brands/qx.png'
import shadowrocket from '@/assets/brands/shadowrocket.png'
import stash from '@/assets/brands/stash.png'
import surge from '@/assets/brands/surge.png'
import telegram from '@/assets/brands/telegram.svg'

// 客户端与第三方服务的官方图标。App 图标本身带底色，直接铺满；标志类（logo）放在白底上。
export const brands: Record<string, { src: string; logo?: boolean }> = {
  surge: { src: surge },
  shadowrocket: { src: shadowrocket },
  loon: { src: loon },
  qx: { src: qx },
  stash: { src: stash },
  egern: { src: egern },
  bark: { src: bark },
  qinglong: { src: qinglong, logo: true },
  telegram: { src: telegram, logo: true },
}
