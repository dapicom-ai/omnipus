import { createFileRoute } from '@tanstack/react-router'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ProvidersSection } from '@/components/settings/ProvidersSection'
import { SecuritySection } from '@/components/settings/SecuritySection'
import { GatewaySection } from '@/components/settings/GatewaySection'
import { DataSection } from '@/components/settings/DataSection'
import { RoutingSection } from '@/components/settings/RoutingSection'
import { ProfileSection } from '@/components/settings/ProfileSection'
import { AboutSection } from '@/components/settings/AboutSection'
import { useAuthStore } from '@/store/auth'
import { DevicesSection } from '@/components/settings/DevicesSection'

function SettingsScreen() {
  const role = useAuthStore((s) => s.role)
  const isAdmin = role === 'admin'

  return (
    <div className="absolute inset-0 overflow-y-auto">
      <div className="max-w-3xl mx-auto px-4 py-6">
        {/* Header */}
        <div className="mb-6">
          <h1 className="font-headline text-2xl font-bold text-[var(--color-secondary)]">Settings</h1>
          <p className="text-sm text-[var(--color-muted)] mt-0.5">
            Configure gateway, credentials, security, and data management.
          </p>
        </div>

        <Tabs defaultValue="providers">
          {/* Sticky tab bar — stays visible while scrolling tab content */}
          <TabsList className="mb-6 flex-wrap h-auto gap-1 sticky top-0 z-10 bg-[var(--color-primary)] py-2 -mx-1 px-1">
            <TabsTrigger value="providers">Providers</TabsTrigger>
            <TabsTrigger value="security">Security</TabsTrigger>
            <TabsTrigger value="gateway">Gateway</TabsTrigger>
            <TabsTrigger value="data">Data</TabsTrigger>
            <TabsTrigger value="routing">Routing</TabsTrigger>
            <TabsTrigger value="profile">Profile</TabsTrigger>
            {isAdmin && <TabsTrigger value="devices">Devices</TabsTrigger>}
            <TabsTrigger value="about">About</TabsTrigger>
          </TabsList>

          <TabsContent value="providers">
            <ProvidersSection />
          </TabsContent>

          <TabsContent value="security">
            <SecuritySection />
          </TabsContent>

          <TabsContent value="gateway">
            <GatewaySection />
          </TabsContent>

          <TabsContent value="data">
            <DataSection />
          </TabsContent>

          <TabsContent value="routing">
            <RoutingSection />
          </TabsContent>

          <TabsContent value="profile">
            <ProfileSection />
          </TabsContent>

          {isAdmin && (
            <TabsContent value="devices">
              <DevicesSection />
            </TabsContent>
          )}

          <TabsContent value="about">
            <AboutSection />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}

export const Route = createFileRoute('/_app/settings')({
  component: SettingsScreen,
})
