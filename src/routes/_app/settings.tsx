import { createFileRoute } from '@tanstack/react-router'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ProvidersSection } from '@/components/settings/ProvidersSection'
import { SecuritySection } from '@/components/settings/SecuritySection'
import { GatewaySection } from '@/components/settings/GatewaySection'
import { DataSection } from '@/components/settings/DataSection'
import { RoutingSection } from '@/components/settings/RoutingSection'
import { ProfileSection } from '@/components/settings/ProfileSection'
import { AboutSection } from '@/components/settings/AboutSection'

function SettingsScreen() {
  return (
    <div className="max-w-3xl mx-auto px-4 py-6">
      <div className="mb-6">
        <h1 className="font-headline text-2xl font-bold text-[var(--color-secondary)]">Settings</h1>
        <p className="text-sm text-[var(--color-muted)] mt-0.5">
          Configure gateway, credentials, security, and data management.
        </p>
      </div>

      <Tabs defaultValue="providers">
        <TabsList className="mb-6 flex-wrap h-auto gap-1">
          <TabsTrigger value="providers">Providers</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="gateway">Gateway</TabsTrigger>
          <TabsTrigger value="data">Data</TabsTrigger>
          <TabsTrigger value="routing">Routing</TabsTrigger>
          <TabsTrigger value="profile">Profile</TabsTrigger>
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

        <TabsContent value="about">
          <AboutSection />
        </TabsContent>
      </Tabs>
    </div>
  )
}

export const Route = createFileRoute('/_app/settings')({
  component: SettingsScreen,
})
