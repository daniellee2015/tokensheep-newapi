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
import type { SetStateAction } from 'react'
import { useTranslation } from 'react-i18next'

import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export type WaffoPancakeSettingsValues = {
  WaffoPancakeMerchantID: string
  WaffoPancakeReturnURL: string
  // TokenSheep additions — see setting/payment_waffo_pancake.go.
  WaffoPancakeApplyUSDExchangeRate: boolean
  WaffoPancakeSurchargePercent: string
}

export interface WaffoPancakeBinding {
  storeID: string
  productID: string
}

interface Props {
  defaultValues: WaffoPancakeSettingsValues
  values: WaffoPancakeSettingsValues
  onValueChange: <K extends keyof WaffoPancakeSettingsValues>(
    key: K,
    value: WaffoPancakeSettingsValues[K]
  ) => void
  selectedBinding: WaffoPancakeBinding
  savedBinding: WaffoPancakeBinding
  onSelectedBindingChange: (value: SetStateAction<WaffoPancakeBinding>) => void
}

const PANCAKE_DASHBOARD_URL = 'https://pancake.waffo.ai/merchant/dashboard'

export function WaffoPancakeSettingsSection({
  values,
  onValueChange,
  selectedBinding,
  savedBinding,
  onSelectedBindingChange,
}: Props) {
  const { t } = useTranslation()

  return (
    <div className='space-y-4 pt-4'>
      <div>
        <h3 className='text-lg font-medium'>{t('Waffo Pancake MoR')}</h3>
        <p className='text-muted-foreground text-sm'>
          {t(
            'Start collecting payments globally without registering a company. Built for indie developers, OPC sole proprietorships, and startups. Waffo Pancake acts as your Merchant of Record, taking on the compliance burden of global payment collection — consumption tax, invoicing, subscription management, refunds, and chargebacks. Solo developers can launch fast and stay focused on product instead of compliance. Onboard in minutes — one prompt to a full integration.'
          )}
        </p>
      </div>

      <div className='grid min-w-0 gap-x-5 gap-y-4 lg:grid-cols-2'>
        <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 lg:col-span-2 dark:bg-blue-950 dark:text-blue-100'>
          <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
          <ul className='list-inside list-disc space-y-1'>
            <li>
              {t('Webhook URL (Test):')}{' '}
              <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                {'<ServerAddress>/api/waffo-pancake/webhook/test'}
              </code>
            </li>
            <li>
              {t('Webhook URL (Production):')}{' '}
              <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                {'<ServerAddress>/api/waffo-pancake/webhook/live'}
              </code>
            </li>
            <li>
              {t(
                'Register each URL into the matching Test Mode / Production Mode webhook slot in the Pancake dashboard. Separate endpoints prevent test traffic from accidentally crediting production accounts.'
              )}
            </li>
            <li>
              {t('Configure at:')}{' '}
              <a
                href={PANCAKE_DASHBOARD_URL}
                target='_blank'
                rel='noreferrer'
                className='underline hover:no-underline'
              >
                {t('Waffo Pancake Dashboard')}
              </a>
            </li>
          </ul>
        </div>

        <div className='grid gap-1.5'>
          <Label>{t('Merchant ID')}</Label>
          <Input
            placeholder='MER_xxx'
            autoComplete='off'
            value={values.WaffoPancakeMerchantID}
            onChange={(event) =>
              onValueChange('WaffoPancakeMerchantID', event.target.value)
            }
          />
        </div>

        <div className='grid gap-1.5'>
          <Label>{t('Payment return URL')}</Label>
          <Input
            placeholder='https://example.com/console/topup'
            value={values.WaffoPancakeReturnURL}
            onChange={(event) =>
              onValueChange('WaffoPancakeReturnURL', event.target.value)
            }
          />
        </div>

        <div className='flex items-start justify-between gap-4 rounded-md border p-3 lg:col-span-2'>
          <div className='space-y-1'>
            <Label htmlFor='waffo-pancake-apply-usd-rate'>
              {t('Apply USD exchange rate')}
            </Label>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Divide the station-side dollar amount by the global USD exchange rate before sending it to Pancake. Match this to the way your station displays balance vs. how Pancake settles.'
              )}
            </p>
          </div>
          <Switch
            id='waffo-pancake-apply-usd-rate'
            checked={values.WaffoPancakeApplyUSDExchangeRate}
            onCheckedChange={(checked) =>
              onValueChange('WaffoPancakeApplyUSDExchangeRate', checked)
            }
          />
        </div>

        <div className='grid gap-1.5 lg:col-span-2'>
          <Label>{t('Pancake surcharge (%)')}</Label>
          <Input
            placeholder='0.5'
            inputMode='decimal'
            value={values.WaffoPancakeSurchargePercent}
            onChange={(event) =>
              onValueChange(
                'WaffoPancakeSurchargePercent',
                event.target.value
              )
            }
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Padding added on top of the Pancake settlement to cover the platform fee (default 0.5%). Only affects the amount charged on Pancake — station-side quota credited stays the selected amount.'
            )}
          </p>
        </div>

        <div className='space-y-4 pt-2 lg:col-span-2'>
          <div>
            <h4 className='font-medium'>
              {t('Bind a Pancake store + product')}
            </h4>
          </div>

          <div className='rounded-md border border-blue-200 bg-blue-50 p-3 text-xs text-blue-900 dark:border-blue-900/60 dark:bg-blue-950/40 dark:text-blue-100'>
            <p className='mb-1 font-medium'>
              {t('Why only one store + product?')}
            </p>
            <ul className='list-inside list-disc space-y-1'>
              <li>
                {t(
                  'The bound Store is the parent container for every Pancake product new-api creates from this admin — both the wallet top-up product and any subscription-plan products. One store is enough; pin a different one only if you genuinely run separate Pancake catalogs.'
                )}
              </li>
              <li>
                {t(
                  'The bound Product powers wallet top-ups: when a user enters any amount, new-api runs the checkout against this single Pancake product and overrides the price per session — no need to pre-create $1 / $5 / $10 SKUs.'
                )}
              </li>
              <li>
                {t(
                  'Subscription plans do NOT use the bound Product — each plan has its own dedicated Pancake product, set in the Subscriptions admin (or auto-minted via the "+ Create" button there).'
                )}
              </li>
            </ul>
          </div>

          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='grid gap-1.5'>
              <Label>{t('Store')}</Label>
              <Input
                placeholder='STO_xxx'
                value={selectedBinding.storeID}
                onChange={(event) =>
                  onSelectedBindingChange((previous) => ({
                    ...previous,
                    storeID: event.target.value,
                  }))
                }
              />
            </div>

            <div className='grid gap-1.5'>
              <Label>{t('Product')}</Label>
              <Input
                placeholder='PROD_xxx'
                value={selectedBinding.productID}
                onChange={(event) =>
                  onSelectedBindingChange((previous) => ({
                    ...previous,
                    productID: event.target.value,
                  }))
                }
              />
            </div>
          </div>

          {savedBinding.storeID || savedBinding.productID ? (
            <div className='text-muted-foreground flex flex-wrap gap-x-3 gap-y-1 text-xs'>
              {savedBinding.storeID ? (
                <span>
                  {t('Bound store:')}{' '}
                  <code className='bg-muted rounded px-1 py-0.5'>
                    {savedBinding.storeID}
                  </code>
                </span>
              ) : null}
              {savedBinding.productID ? (
                <span>
                  {t('Bound product:')}{' '}
                  <code className='bg-muted rounded px-1 py-0.5'>
                    {savedBinding.productID}
                  </code>
                </span>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}
