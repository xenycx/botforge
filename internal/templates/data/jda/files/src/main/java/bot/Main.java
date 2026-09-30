package bot;

import java.util.EnumSet;

import net.dv8tion.jda.api.JDA;
import net.dv8tion.jda.api.JDABuilder;
import net.dv8tion.jda.api.events.interaction.command.SlashCommandInteractionEvent;
import net.dv8tion.jda.api.hooks.ListenerAdapter;
import net.dv8tion.jda.api.interactions.commands.build.Commands;
import net.dv8tion.jda.api.requests.GatewayIntent;

/** JDA starter. Set DISCORD_TOKEN in the panel's Environment tab. */
public class Main extends ListenerAdapter {
    public static void main(String[] args) throws InterruptedException {
        String token = System.getenv("DISCORD_TOKEN");
        if (token == null || token.isBlank()) {
            System.err.println("DISCORD_TOKEN is not set. Add it in the panel under Environment.");
            System.exit(1);
        }
        JDA jda = JDABuilder.createLight(token, EnumSet.noneOf(GatewayIntent.class))
                .addEventListeners(new Main())
                .build();
        jda.updateCommands()
                .addCommands(Commands.slash("ping", "Replies with the gateway latency"))
                .queue();
        jda.awaitReady();
        System.out.println("Logged in as " + jda.getSelfUser().getName());
    }

    @Override
    public void onSlashCommandInteraction(SlashCommandInteractionEvent event) {
        if (event.getName().equals("ping")) {
            event.reply("Pong! " + event.getJDA().getGatewayPing() + " ms").queue();
        }
    }
}
