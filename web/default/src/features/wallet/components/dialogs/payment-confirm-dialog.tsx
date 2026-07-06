/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'

import { formatCurrency, getPaymentIcon } from '../../lib'
import type { PaymentMethod } from '../../types'

// TokenSheep confirm dialog. Station-side ($ = quota) rows only; the real
// USD settlement, the exchange-rate conversion, and per-payment-method
// fees are shown on the Waffo checkout page after redirect.
//
// Rows (top to bottom):
//   1. Topup amount              — station $, same as the field the user typed.
//   2. You pay                   — station $, same as row 1 (1:1 quota).
//   3. Pancake surcharge (+X%)   — informational, green, from WaffoPancakeSurchargePercent.
//   4. Payment method            — carried over from the recharge form.
interface PaymentConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onConfirm: () => void
  topupAmount: number
  // Kept for source-compat with the parent; not rendered. See file comment.
  paymentAmount?: number
  paymentMethod: PaymentMethod | undefined
  calculating: boolean
  processing: boolean
  // Legacy props kept for parent compatibility; not rendered.
  discountRate?: number
  usdExchangeRate?: number
  /**
   * Waffo Pancake surcharge percent (from options: WaffoPancakeSurchargePercent).
   * Shown as a small green "+0.5%" chip — the extra is charged on the
   * provider's checkout page, not station-side.
   */
  pancakeSurchargePercent?: number
}

export function PaymentConfirmDialog({
  open,
  onOpenChange,
  onConfirm,
  topupAmount,
  paymentMethod,
  processing,
  pancakeSurchargePercent = 0,
}: PaymentConfirmDialogProps) {
  const { t } = useTranslation()

  const surchargeVisible =
    pancakeSurchargePercent > 0 && paymentMethod?.type === 'waffo_pancake'

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className='max-sm:w-[calc(100vw-1.5rem)] sm:max-w-md'>
        <AlertDialogHeader>
          <AlertDialogTitle className='text-xl font-semibold'>
            {t('Confirm Payment')}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t('Review your payment details')}
          </AlertDialogDescription>
        </AlertDialogHeader>

        <div className='space-y-3 py-3 sm:space-y-4 sm:py-4'>
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-sm'>
              {t('Topup Amount')}
            </span>
            <span className='text-lg font-semibold'>
              {formatCurrency(topupAmount)}
            </span>
          </div>

          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-sm'>
              {t('You Pay')}
            </span>
            <span className='text-2xl font-semibold'>
              {formatCurrency(topupAmount)}
            </span>
          </div>

          {surchargeVisible && (
            <div className='flex items-center justify-between rounded-md bg-green-500/10 px-3 py-2 text-sm'>
              <span className='text-green-700 dark:text-green-400'>
                {t('Payment provider fee')}
              </span>
              <span className='font-semibold text-green-700 dark:text-green-400'>
                +{pancakeSurchargePercent}%
              </span>
            </div>
          )}

          <div className='border-t pt-4'>
            <div className='flex items-center justify-between'>
              <span className='text-muted-foreground text-sm'>
                {t('Payment Method')}
              </span>
              <div className='flex items-center gap-2'>
                {getPaymentIcon(
                  paymentMethod?.type,
                  'h-4 w-4',
                  paymentMethod?.icon,
                  paymentMethod?.name
                )}
                <span className='font-medium'>{paymentMethod?.name}</span>
              </div>
            </div>
          </div>
        </div>

        <AlertDialogFooter className='grid grid-cols-2 gap-2 sm:flex'>
          <AlertDialogCancel disabled={processing}>
            {t('Cancel')}
          </AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} disabled={processing}>
            {processing && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {t('Confirm Payment')}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
